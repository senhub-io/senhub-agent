package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc"
	grpcgzip "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// metricBatchForwarder ships one batch of raw ResourceLogs to the logs
// endpoint. Two implementations, one per OTLP transport — the exact
// analogue of spanForwarder, and for the same reason: the SDK log
// exporter only accepts SDK records built from the agent's own Resource,
// so relaying an application's records through it would replace their
// identity with the agent's.
type metricBatchForwarder interface {
	forward(ctx context.Context, rm []*metricpb.ResourceMetrics) error
	close() error
}

// ── gRPC transport ───────────────────────────────────────────────────

type grpcMetricBatchForwarder struct {
	conn    *grpc.ClientConn
	client  collectormetricspb.MetricsServiceClient
	headers map[string]string
}

func newGRPCMetricBatchForwarder(cfg Config) (*grpcMetricBatchForwarder, error) {
	rt := resolveTransport(cfg, cfg.Metrics.SignalTransport)
	creds, _, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("metrics relay TLS credentials: %w", err)
	}
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if cfg.Compression == "gzip" {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.UseCompressor(grpcgzip.Name)))
	}
	conn, err := grpc.NewClient(rt.endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("metrics relay gRPC client for %s: %w", rt.endpoint, err)
	}
	return &grpcMetricBatchForwarder{
		conn:    conn,
		client:  collectormetricspb.NewMetricsServiceClient(conn),
		headers: rt.headers,
	}, nil
}

func (f *grpcMetricBatchForwarder) forward(ctx context.Context, rm []*metricpb.ResourceMetrics) error {
	if len(f.headers) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(f.headers))
	}
	if _, err := f.client.Export(ctx, &collectormetricspb.ExportMetricsServiceRequest{ResourceMetrics: rm}); err != nil {
		return fmt.Errorf("MetricsService.Export: %w", err)
	}
	return nil
}

func (f *grpcMetricBatchForwarder) close() error {
	return f.conn.Close()
}

// ── HTTP transport ───────────────────────────────────────────────────

type httpMetricBatchForwarder struct {
	client  *http.Client
	url     string
	headers map[string]string
	gzip    bool
}

func newHTTPMetricBatchForwarder(cfg Config) (*httpMetricBatchForwarder, error) {
	rt := resolveTransport(cfg, cfg.Metrics.SignalTransport)
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("metrics relay TLS config: %w", err)
	}
	scheme := "https"
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if insec {
		scheme = "http"
	} else {
		transport.TLSClientConfig = tlsConf
	}
	return &httpMetricBatchForwarder{
		client:  &http.Client{Transport: transport},
		url:     scheme + "://" + rt.endpoint + "/v1/metrics",
		headers: rt.headers,
		gzip:    cfg.Compression == "gzip",
	}, nil
}

func (f *httpMetricBatchForwarder) forward(ctx context.Context, rm []*metricpb.ResourceMetrics) error {
	body, err := proto.Marshal(&collectormetricspb.ExportMetricsServiceRequest{ResourceMetrics: rm})
	if err != nil {
		return fmt.Errorf("marshal ExportMetricsServiceRequest: %w", err)
	}
	if f.gzip {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(body); err != nil {
			return fmt.Errorf("gzip request body: %w", err)
		}
		if err := zw.Close(); err != nil {
			return fmt.Errorf("gzip request body: %w", err)
		}
		body = buf.Bytes()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build logs POST for %s: %w", f.url, err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if f.gzip {
		req.Header.Set("Content-Encoding", "gzip")
	}
	for k, v := range f.headers {
		req.Header.Set(k, v)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", f.url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: unexpected status %d", f.url, resp.StatusCode)
	}
	return nil
}

func (f *httpMetricBatchForwarder) close() error {
	f.client.CloseIdleConnections()
	return nil
}

// ── Relay pump ───────────────────────────────────────────────────────

// metricsRelay drains the agentstate verbatim-log channel (fed by the
// otlp_receiver probe) and forwards each raw ResourceLogs batch to the
// logs endpoint, preserving the emitting application's Resource.
//
// It runs ALONGSIDE logsPump, which carries the agent's OWN records
// through the SDK pipeline with the agent's Resource. The two never see
// the same record: the receiver publishes ingested logs on the verbatim
// channel and logsPump skips them (see logs.go), so an ingested record is
// relayed exactly once, with its identity intact.
//
// Gated by the same signals.logs.enabled flag as the SDK pipeline.
type metricsRelay struct {
	cfg       Config
	forwarder metricBatchForwarder
	enricher  *relayEnricher
	logger    *logger.ModuleLogger

	mu         sync.Mutex
	subscribed <-chan []*metricpb.ResourceMetrics
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newMetricsRelay(cfg Config, enricher *relayEnricher, moduleLogger *logger.ModuleLogger) (*metricsRelay, error) {
	var fwd metricBatchForwarder
	var err error
	if cfg.Protocol == "http" {
		fwd, err = newHTTPMetricBatchForwarder(cfg)
	} else {
		fwd, err = newGRPCMetricBatchForwarder(cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("building log batch forwarder: %w", err)
	}
	return &metricsRelay{cfg: cfg, forwarder: fwd, enricher: enricher, logger: moduleLogger}, nil
}

// start subscribes to the agentstate verbatim-log channel and launches the
// drain goroutine. Idempotent.
func (r *metricsRelay) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribed != nil {
		return
	}
	ch := agentstate.SubscribeMetricBatchesFor(strategyName, defaultMetricRelayBufferSize)
	r.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.drain(ctx, ch)
}

// drain accumulates ResourceLogs and flushes on the record-count budget or
// the batch timeout, mirroring the span relay's batching contract.
func (r *metricsRelay) drain(ctx context.Context, ch <-chan []*metricpb.ResourceMetrics) {
	defer r.wg.Done()

	timeout := r.cfg.Metrics.Interval
	if timeout <= 0 {
		timeout = DefaultMetricsInterval
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	var pending []*metricpb.ResourceMetrics
	pendingPoints := 0
	pendingBytes := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		r.export(pending, pendingPoints)
		pending = nil
		pendingPoints = 0
		pendingBytes = 0
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case rm, ok := <-ch:
			if !ok {
				flush()
				return
			}
			pending = append(pending, rm...)
			pendingPoints += countMetricPoints(rm)
			pendingBytes += metricBatchBytes(rm)
			// Only a byte budget here: unlike logs and spans, the metric
			// signal has no operator-facing batch-size knob — its cadence
			// is the push interval, so the ticker is the primary trigger
			// and the byte budget only guards against few-but-huge batches
			// growing the pending buffer past one accepted request.
			if pendingBytes >= maxPendingMetricBytes {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// maxPendingMetricBytes bounds the drain's in-flight accumulation between
// flushes, matching the span relay's cap.
const maxPendingMetricBytes = 4 * 1024 * 1024

func metricBatchBytes(rm []*metricpb.ResourceMetrics) int {
	n := 0
	for _, r := range rm {
		n += proto.Size(r)
	}
	return n
}

// countMetricPoints totals the data points across a ResourceMetrics batch
// — the unit the relayed self-metric is expressed in. Every point type is
// counted so the figure is comparable with the receiver's ingest count.
func countMetricPoints(rm []*metricpb.ResourceMetrics) int {
	n := 0
	for _, r := range rm {
		for _, sm := range r.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				switch d := m.GetData().(type) {
				case *metricpb.Metric_Gauge:
					n += len(d.Gauge.GetDataPoints())
				case *metricpb.Metric_Sum:
					n += len(d.Sum.GetDataPoints())
				case *metricpb.Metric_Histogram:
					n += len(d.Histogram.GetDataPoints())
				case *metricpb.Metric_ExponentialHistogram:
					n += len(d.ExponentialHistogram.GetDataPoints())
				case *metricpb.Metric_Summary:
					n += len(d.Summary.GetDataPoints())
				}
			}
		}
	}
	return n
}

// defaultMetricRelayBufferSize sizes the relay's receive channel. Not
// operator-facing: the metric signal exposes an interval, not buffering
// knobs, and the drop-oldest posture makes the exact depth uncritical.
const defaultMetricRelayBufferSize = 2048

// export ships one accumulated batch with a bounded per-call timeout. A
// failed export drops the batch (no retry — best-effort, matching the
// receive side's drop-oldest posture) and counts an OTLP export error.
func (r *metricsRelay) export(rm []*metricpb.ResourceMetrics, points int) {
	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Agent context is ADDED, never substituted: the emitting app's
	// Resource is what makes its records attributable downstream.
	rm = r.enricher.enrichMetrics(rm)

	if err := r.forwarder.forward(ctx, rm); err != nil {
		agentstate.IncrementOTLPExportErrors()
		r.logger.Warn().
			Str("error", redactSensitive(err.Error())).
			Int("resource_metrics", len(rm)).
			Int("points", points).
			Msg("OTLP metric relay export failed; batch dropped")
		return
	}
	agentstate.IncrementOTLPMetricsRelayed(points)
	r.logger.Debug().
		Int("resource_metrics", len(rm)).
		Int("points", points).
		Msg("OTLP metrics relayed")
}

// stop cancels the drain goroutine, unsubscribes, waits for the final
// flush (bounded), and closes the forwarder. Idempotent.
func (r *metricsRelay) stop(ctx context.Context) {
	r.mu.Lock()
	ch := r.subscribed
	cancel := r.cancel
	r.subscribed = nil
	r.cancel = nil
	r.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	if ch != nil {
		agentstate.UnsubscribeMetricBatches(ch)
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}

	if err := r.forwarder.close(); err != nil {
		r.logger.Warn().Err(err).Msg("OTLP metric relay forwarder close failed")
	}
}

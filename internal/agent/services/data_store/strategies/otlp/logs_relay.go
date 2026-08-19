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

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc"
	grpcgzip "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// logBatchForwarder ships one batch of raw ResourceLogs to the logs
// endpoint. Two implementations, one per OTLP transport — the exact
// analogue of spanForwarder, and for the same reason: the SDK log
// exporter only accepts SDK records built from the agent's own Resource,
// so relaying an application's records through it would replace their
// identity with the agent's.
type logBatchForwarder interface {
	forward(ctx context.Context, rl []*logspb.ResourceLogs) error
	close() error
}

// ── gRPC transport ───────────────────────────────────────────────────

type grpcLogBatchForwarder struct {
	conn    *grpc.ClientConn
	client  collectorlogspb.LogsServiceClient
	headers map[string]string
}

func newGRPCLogBatchForwarder(cfg Config) (*grpcLogBatchForwarder, error) {
	rt := resolveTransport(cfg, cfg.Logs.SignalTransport)
	creds, _, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("logs relay TLS credentials: %w", err)
	}
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if cfg.Compression == "gzip" {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.UseCompressor(grpcgzip.Name)))
	}
	conn, err := grpc.NewClient(rt.endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("logs relay gRPC client for %s: %w", rt.endpoint, err)
	}
	return &grpcLogBatchForwarder{
		conn:    conn,
		client:  collectorlogspb.NewLogsServiceClient(conn),
		headers: rt.headers,
	}, nil
}

func (f *grpcLogBatchForwarder) forward(ctx context.Context, rl []*logspb.ResourceLogs) error {
	if len(f.headers) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(f.headers))
	}
	if _, err := f.client.Export(ctx, &collectorlogspb.ExportLogsServiceRequest{ResourceLogs: rl}); err != nil {
		return fmt.Errorf("LogsService.Export: %w", err)
	}
	return nil
}

func (f *grpcLogBatchForwarder) close() error {
	return f.conn.Close()
}

// ── HTTP transport ───────────────────────────────────────────────────

type httpLogBatchForwarder struct {
	client  *http.Client
	url     string
	headers map[string]string
	gzip    bool
}

func newHTTPLogBatchForwarder(cfg Config) (*httpLogBatchForwarder, error) {
	rt := resolveTransport(cfg, cfg.Logs.SignalTransport)
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("logs relay TLS config: %w", err)
	}
	scheme := "https"
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if insec {
		scheme = "http"
	} else {
		transport.TLSClientConfig = tlsConf
	}
	return &httpLogBatchForwarder{
		client:  &http.Client{Transport: transport},
		url:     scheme + "://" + rt.endpoint + "/v1/logs",
		headers: rt.headers,
		gzip:    cfg.Compression == "gzip",
	}, nil
}

func (f *httpLogBatchForwarder) forward(ctx context.Context, rl []*logspb.ResourceLogs) error {
	body, err := proto.Marshal(&collectorlogspb.ExportLogsServiceRequest{ResourceLogs: rl})
	if err != nil {
		return fmt.Errorf("marshal ExportLogsServiceRequest: %w", err)
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

func (f *httpLogBatchForwarder) close() error {
	f.client.CloseIdleConnections()
	return nil
}

// ── Relay pump ───────────────────────────────────────────────────────

// logsRelay drains the agentstate verbatim-log channel (fed by the
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
type logsRelay struct {
	cfg       Config
	forwarder logBatchForwarder
	enricher  *relayEnricher
	logger    *logger.ModuleLogger

	mu         sync.Mutex
	subscribed <-chan []*logspb.ResourceLogs
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newLogsRelay(cfg Config, enricher *relayEnricher, moduleLogger *logger.ModuleLogger) (*logsRelay, error) {
	var fwd logBatchForwarder
	var err error
	if cfg.Protocol == "http" {
		fwd, err = newHTTPLogBatchForwarder(cfg)
	} else {
		fwd, err = newGRPCLogBatchForwarder(cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("building log batch forwarder: %w", err)
	}
	return &logsRelay{cfg: cfg, forwarder: fwd, enricher: enricher, logger: moduleLogger}, nil
}

// start subscribes to the agentstate verbatim-log channel and launches the
// drain goroutine. Idempotent.
func (r *logsRelay) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribed != nil {
		return
	}
	ch := agentstate.SubscribeLogBatchesFor(strategyName, r.cfg.Logs.BufferSize)
	r.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.drain(ctx, ch)
}

// drain accumulates ResourceLogs and flushes on the record-count budget or
// the batch timeout, mirroring the span relay's batching contract.
func (r *logsRelay) drain(ctx context.Context, ch <-chan []*logspb.ResourceLogs) {
	defer r.wg.Done()

	timeout := r.cfg.Logs.BatchTimeout
	if timeout <= 0 {
		timeout = DefaultLogsBatchTimeout
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	var pending []*logspb.ResourceLogs
	pendingRecords := 0
	pendingBytes := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		r.export(pending, pendingRecords)
		pending = nil
		pendingRecords = 0
		pendingBytes = 0
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case rl, ok := <-ch:
			if !ok {
				flush()
				return
			}
			pending = append(pending, rl...)
			pendingRecords += countLogRecords(rl)
			pendingBytes += logBatchBytes(rl)
			// Same dual trigger as the span relay: a record budget plus a
			// byte budget, so few-but-huge records cannot grow the pending
			// buffer past what one accepted request may hold.
			if (r.cfg.Logs.BatchSize > 0 && pendingRecords >= r.cfg.Logs.BatchSize) ||
				pendingBytes >= maxPendingLogBytes {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// maxPendingLogBytes bounds the drain's in-flight accumulation between
// flushes, matching the span relay's cap.
const maxPendingLogBytes = 4 * 1024 * 1024

// relayedLogProbeType is the producer probe type whose records travel the
// verbatim relay rather than the SDK pipeline. Named here, next to the
// relay that owns the contract, so the reason the logs pump skips them is
// readable from one place.
const relayedLogProbeType = "otlp_receiver"

func logBatchBytes(rl []*logspb.ResourceLogs) int {
	n := 0
	for _, r := range rl {
		n += proto.Size(r)
	}
	return n
}

// countLogRecords totals the records across a ResourceLogs batch — the
// unit Logs.BatchSize and the relayed self-metric are expressed in.
func countLogRecords(rl []*logspb.ResourceLogs) int {
	n := 0
	for _, r := range rl {
		for _, sl := range r.GetScopeLogs() {
			n += len(sl.GetLogRecords())
		}
	}
	return n
}

// export ships one accumulated batch with a bounded per-call timeout. A
// failed export drops the batch (no retry — best-effort, matching the
// receive side's drop-oldest posture) and counts an OTLP export error.
func (r *logsRelay) export(rl []*logspb.ResourceLogs, records int) {
	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Agent context is ADDED, never substituted: the emitting app's
	// Resource is what makes its records attributable downstream.
	rl = r.enricher.enrichLogs(rl)

	if err := r.forwarder.forward(ctx, rl); err != nil {
		agentstate.IncrementOTLPExportErrors("logs")
		r.logger.Warn().
			Str("error", redactSensitive(err.Error())).
			Int("resource_logs", len(rl)).
			Int("records", records).
			Msg("OTLP log relay export failed; batch dropped")
		return
	}
	agentstate.IncrementOTLPLogsRelayed(records)
	r.logger.Debug().
		Int("resource_logs", len(rl)).
		Int("records", records).
		Msg("OTLP logs relayed")
}

// stop cancels the drain goroutine, unsubscribes, waits for the final
// flush (bounded), and closes the forwarder. Idempotent.
func (r *logsRelay) stop(ctx context.Context) {
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
		agentstate.UnsubscribeLogBatches(ch)
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
		r.logger.Warn().Err(err).Msg("OTLP log relay forwarder close failed")
	}
}

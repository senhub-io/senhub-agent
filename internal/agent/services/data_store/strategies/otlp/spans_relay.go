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

	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	grpcgzip "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// spanForwarder ships one batch of raw ResourceSpans to the traces
// endpoint. Two implementations, one per OTLP transport. The SDK trace
// exporter (tracesPipeline) cannot be reused here: it only accepts SDK
// ReadOnlySpan values produced by the agent's own tracer, and rebuilding
// received proto spans into SDK spans would be lossy — so the relay
// speaks the collector TracesService protocol directly.
type spanForwarder interface {
	forward(ctx context.Context, rs []*tracepb.ResourceSpans) error
	close() error
}

// ── gRPC transport ───────────────────────────────────────────────────

// grpcSpanForwarder calls TraceService.Export over its own lazily-dialed
// gRPC connection, built from the same resolved transport values
// (endpoint / TLS credentials / headers / compression) the SDK exporters
// use — resolveTransport + tlsCredentials are shared with client.go.
type grpcSpanForwarder struct {
	conn    *grpc.ClientConn
	client  collectortracepb.TraceServiceClient
	headers map[string]string
}

func newGRPCSpanForwarder(cfg Config) (*grpcSpanForwarder, error) {
	rt := resolveTransport(cfg, cfg.Traces.SignalTransport)
	creds, _, err := tlsCredentials(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("traces relay TLS credentials: %w", err)
	}
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if cfg.Compression == "gzip" {
		dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(grpc.UseCompressor(grpcgzip.Name)))
	}
	// grpc.NewClient is lazy — no socket opens until the first Export,
	// matching the SDK exporters' construction contract.
	conn, err := grpc.NewClient(rt.endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("traces relay gRPC client for %s: %w", rt.endpoint, err)
	}
	return &grpcSpanForwarder{
		conn:    conn,
		client:  collectortracepb.NewTraceServiceClient(conn),
		headers: rt.headers,
	}, nil
}

func (f *grpcSpanForwarder) forward(ctx context.Context, rs []*tracepb.ResourceSpans) error {
	if len(f.headers) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(f.headers))
	}
	if _, err := f.client.Export(ctx, &collectortracepb.ExportTraceServiceRequest{ResourceSpans: rs}); err != nil {
		return fmt.Errorf("TraceService.Export: %w", err)
	}
	return nil
}

func (f *grpcSpanForwarder) close() error {
	return f.conn.Close()
}

// ── HTTP transport ───────────────────────────────────────────────────

// httpSpanForwarder POSTs the marshalled ExportTraceServiceRequest to the
// spec OTLP/HTTP traces path with the resolved headers/TLS, gzipping the
// body when the strategy compression is gzip (same wire shape as the SDK
// OTLP/HTTP exporters).
type httpSpanForwarder struct {
	client  *http.Client
	url     string
	headers map[string]string
	gzip    bool
}

func newHTTPSpanForwarder(cfg Config) (*httpSpanForwarder, error) {
	rt := resolveTransport(cfg, cfg.Traces.SignalTransport)
	tlsConf, insec, err := buildTLSConfig(rt.tls)
	if err != nil {
		return nil, fmt.Errorf("traces relay TLS config: %w", err)
	}
	scheme := "https"
	// Honor HTTP(S)_PROXY like the SDK OTLP/HTTP exporters do, so the span
	// relay follows the same egress path as the metrics/logs exporters.
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if insec {
		scheme = "http"
	} else {
		transport.TLSClientConfig = tlsConf
	}
	return &httpSpanForwarder{
		client:  &http.Client{Transport: transport},
		url:     scheme + "://" + rt.endpoint + "/v1/traces",
		headers: rt.headers,
		gzip:    cfg.Compression == "gzip",
	}, nil
}

func (f *httpSpanForwarder) forward(ctx context.Context, rs []*tracepb.ResourceSpans) error {
	body, err := proto.Marshal(&collectortracepb.ExportTraceServiceRequest{ResourceSpans: rs})
	if err != nil {
		return fmt.Errorf("marshal ExportTraceServiceRequest: %w", err)
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
		return fmt.Errorf("build traces POST for %s: %w", f.url, err)
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
	// Drain so the connection can be reused; the response body (an
	// ExportTraceServiceResponse) carries nothing the relay acts on.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: unexpected status %d", f.url, resp.StatusCode)
	}
	return nil
}

func (f *httpSpanForwarder) close() error {
	f.client.CloseIdleConnections()
	return nil
}

// ── Relay pump ───────────────────────────────────────────────────────

// spansRelay drains the agentstate span channel (fed by the otlp_receiver
// probe) and forwards each raw ResourceSpans batch to the traces endpoint
// — the export half of the OTLP-in → OTLP-out trace relay.
//
// Independent of the SDK tracesPipeline, which exports the agent's OWN
// spans through a BatchSpanProcessor; both may be active at once. Gated
// by the same signals.traces.enabled flag as the SDK pipeline — there is
// deliberately no separate relay config key: enabling the traces signal
// means "this strategy ships traces", whatever their origin.
//
// Same lifecycle contract as logsPump: start subscribes and launches the
// drain goroutine (idempotent), stop cancels, unsubscribes, flushes the
// pending batch best-effort, and closes the forwarder.
type spansRelay struct {
	cfg       Config
	forwarder spanForwarder
	logger    *logger.ModuleLogger

	mu         sync.Mutex
	subscribed <-chan []*tracepb.ResourceSpans
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newSpansRelay(cfg Config, moduleLogger *logger.ModuleLogger) (*spansRelay, error) {
	var fwd spanForwarder
	var err error
	if cfg.Protocol == "http" {
		fwd, err = newHTTPSpanForwarder(cfg)
	} else {
		fwd, err = newGRPCSpanForwarder(cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("building span forwarder: %w", err)
	}
	return &spansRelay{cfg: cfg, forwarder: fwd, logger: moduleLogger}, nil
}

// start subscribes to the agentstate span channel and launches the drain
// goroutine. Idempotent — calling twice is a no-op.
func (r *spansRelay) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscribed != nil {
		return
	}
	ch := agentstate.SubscribeSpans(r.cfg.Traces.BufferSize)
	r.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.drain(ctx, ch)
}

// drain accumulates ResourceSpans and flushes when the accumulated span
// count reaches Traces.BatchSize or the Traces.BatchTimeout ticker fires
// — the same batching contract the SDK BatchSpanProcessor applies to the
// agent's own spans. Runs until the context is cancelled OR the channel
// closes, with a final best-effort flush on exit.
func (r *spansRelay) drain(ctx context.Context, ch <-chan []*tracepb.ResourceSpans) {
	defer r.wg.Done()

	timeout := r.cfg.Traces.BatchTimeout
	if timeout <= 0 {
		timeout = DefaultTracesBatchTimeout
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	var pending []*tracepb.ResourceSpans
	pendingSpans := 0
	pendingBytes := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		r.export(pending, pendingSpans)
		pending = nil
		pendingSpans = 0
		pendingBytes = 0
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case rs, ok := <-ch:
			if !ok {
				flush()
				return
			}
			pending = append(pending, rs...)
			pendingSpans += countSpans(rs)
			pendingBytes += batchBytes(rs)
			// Flush on span count OR a byte budget — the byte budget caps
			// the pending buffer even when a sender pushes few-but-huge
			// spans (which the span-count trigger alone would let grow to
			// BatchSize × maxRecvMsgBytes). The channel buffer stays the
			// coarse cap; this keeps the in-drain accumulation bounded.
			if (r.cfg.Traces.BatchSize > 0 && pendingSpans >= r.cfg.Traces.BatchSize) ||
				pendingBytes >= maxPendingSpanBytes {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// maxPendingSpanBytes bounds the drain's in-flight accumulation between
// flushes so a slow/down endpoint plus large payloads cannot grow it without
// limit. Matches the receiver's per-request cap (a single accepted batch is
// already <= maxRecvMsgBytes).
const maxPendingSpanBytes = 4 * 1024 * 1024

// batchBytes returns the marshalled size of a ResourceSpans slice — the unit
// the byte budget is expressed in.
func batchBytes(rs []*tracepb.ResourceSpans) int {
	n := 0
	for _, r := range rs {
		n += proto.Size(r)
	}
	return n
}

// export ships one accumulated batch with a bounded per-call timeout. A
// failed export drops the batch (no retry — the relay is best-effort,
// matching the receive side's drop-oldest posture) and counts an OTLP
// export error. Uses its own context so the final flush during stop
// still runs after the drain context is cancelled.
func (r *spansRelay) export(rs []*tracepb.ResourceSpans, spans int) {
	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := r.forwarder.forward(ctx, rs); err != nil {
		agentstate.IncrementOTLPExportErrors()
		r.logger.Warn().
			Str("error", redactSensitive(err.Error())).
			Int("resource_spans", len(rs)).
			Int("spans", spans).
			Msg("OTLP span relay export failed; batch dropped")
		return
	}
	r.logger.Debug().
		Int("resource_spans", len(rs)).
		Int("spans", spans).
		Msg("OTLP spans relayed")
}

// stop cancels the drain goroutine, unsubscribes, waits for the final
// flush (bounded), and closes the forwarder. Idempotent.
func (r *spansRelay) stop(ctx context.Context) {
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
		agentstate.UnsubscribeSpans(ch)
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// Best-effort; the goroutine exits on its own after the cancel.
	case <-time.After(5 * time.Second):
	}

	if err := r.forwarder.close(); err != nil {
		r.logger.Warn().Err(err).Msg("OTLP span relay forwarder close failed")
	}
}

// countSpans returns the number of spans across a ResourceSpans batch —
// the unit Traces.BatchSize is expressed in (mirrors the SDK's
// MaxExportBatchSize semantics).
func countSpans(rs []*tracepb.ResourceSpans) int {
	n := 0
	for _, r := range rs {
		for _, ss := range r.GetScopeSpans() {
			n += len(ss.GetSpans())
		}
	}
	return n
}

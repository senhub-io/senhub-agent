package otlpreceiver

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// Request/response payload encodings accepted on OTLP/HTTP.
const (
	contentFormatProtobuf = "protobuf"
	contentFormatJSON     = "json"
)

// httpReceiver wraps an *http.Server so the probe can hold it behind a
// small shutdown-capable interface (the probe struct doesn't import
// net/http directly).
type httpReceiver struct {
	server *http.Server
}

func (h *httpReceiver) shutdown(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}

func (p *OTLPReceiverProbe) startHTTP(quitChannel chan struct{}) error {
	lis, err := p.listen()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	// Collected as they are mounted so the startup log names every route
	// actually served. Logging only the metrics path read as "traces is
	// not wired" to an operator checking exactly that.
	var mounted []string
	if p.config.Signals.Metrics {
		mux.HandleFunc(p.config.HTTPPath, p.handleMetrics)
		mounted = append(mounted, p.config.HTTPPath)
	}
	if p.config.Signals.Logs {
		mux.HandleFunc(httpLogsPath, p.handleLogs)
		mounted = append(mounted, httpLogsPath)
	}
	if p.config.Signals.Traces {
		mux.HandleFunc(httpTracesPath, p.handleTraces)
		mounted = append(mounted, httpTracesPath)
	}

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpReadTimeoutS * time.Second,
		WriteTimeout: httpReadTimeoutS * time.Second,
	}

	receiver := &httpReceiver{server: server}

	p.mu.Lock()
	p.httpServer = receiver
	p.listener = lis
	p.mu.Unlock()

	p.markServing()
	go func() {
		serveErr := server.Serve(lis)
		if serveErr != nil && serveErr != http.ErrServerClosed {
			p.moduleLogger.Error().Err(serveErr).Msg("OTLP HTTP server stopped with error")
			p.markStopped(serveErr)
			return
		}
		p.markStopped(nil)
	}()

	go func() {
		<-quitChannel
		p.moduleLogger.Info().Msg("Received quit signal, stopping OTLP HTTP receiver")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	p.moduleLogger.Info().
		Str("address", p.config.Address).
		Strs("paths", mounted).
		Strs("signals", p.config.Signals.names()).
		Msg("OTLP HTTP receiver started")
	return nil
}

// refuseHTTP logs (rate-limited) and counts one HTTP refusal, then writes
// the error response. Retry-After is set for the retryable 429.
func (p *OTLPReceiverProbe) refuseHTTP(w http.ResponseWriter, r *http.Request, signal string, status int, reason, msg string) {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	p.refusals.log(p.moduleLogger, signal, status, reason, remote)
	agentstate.IncrementOTLPReceiverRefused(signal, reason)
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", strconv.Itoa(p.guard.retryAfterSeconds()))
	}
	http.Error(w, msg, status)
}

// authorizeAndReadBody runs the shared ingress preamble for an OTLP/HTTP
// handler: guard (bearer/CIDR/rate-limit), POST-only, content type
// (application/x-protobuf or application/json, parameters allowed), and a
// size-capped body read (identity or gzip; the cap applies to the
// decompressed size). It writes the error response itself and returns
// ok=false when the request must not proceed. The returned format is the
// request payload encoding, which also selects the response encoding.
func (p *OTLPReceiverProbe) authorizeAndReadBody(w http.ResponseWriter, r *http.Request, signal string) ([]byte, string, bool) {
	if err := p.guard.allow(r.RemoteAddr, r.Header.Get("Authorization")); err != nil {
		code := http.StatusInternalServerError
		reason := "guard_error"
		switch {
		case errors.Is(err, errUnauthorized):
			code = http.StatusUnauthorized
			reason = "unauthorized"
		case errors.Is(err, errForbidden):
			code = http.StatusForbidden
			reason = "forbidden"
		case errors.Is(err, errRateLimited):
			code = http.StatusTooManyRequests
			reason = "rate_limited"
		}
		p.refuseHTTP(w, r, signal, code, reason, http.StatusText(code))
		return nil, "", false
	}

	if r.Method != http.MethodPost {
		p.refuseHTTP(w, r, signal, http.StatusMethodNotAllowed, "method", "method not allowed")
		return nil, "", false
	}

	format := contentFormatProtobuf
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			p.refuseHTTP(w, r, signal, http.StatusUnsupportedMediaType, "media_type", "unparseable Content-Type")
			return nil, "", false
		}
		switch strings.ToLower(mediaType) {
		case "application/x-protobuf":
			format = contentFormatProtobuf
		case "application/json":
			format = contentFormatJSON
		default:
			p.refuseHTTP(w, r, signal, http.StatusUnsupportedMediaType, "media_type", "only application/x-protobuf and application/json are supported")
			return nil, "", false
		}
	}

	raw := &io.LimitedReader{R: r.Body, N: maxRecvMsgBytes + 1}
	var reader io.Reader = raw
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(reader)
		if err != nil {
			if raw.N == 0 {
				p.refuseHTTP(w, r, signal, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
				return nil, "", false
			}
			p.refuseHTTP(w, r, signal, http.StatusBadRequest, "bad_body", "invalid gzip body")
			return nil, "", false
		}
		defer func() {
			if cerr := zr.Close(); cerr != nil {
				p.moduleLogger.Debug().Err(cerr).Msg("closing gzip reader")
			}
		}()
		reader = io.LimitReader(zr, maxRecvMsgBytes+1)
	default:
		p.refuseHTTP(w, r, signal, http.StatusUnsupportedMediaType, "encoding", "unsupported Content-Encoding")
		return nil, "", false
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		if raw.N == 0 {
			p.refuseHTTP(w, r, signal, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			return nil, "", false
		}
		p.refuseHTTP(w, r, signal, http.StatusBadRequest, "bad_body", "failed to read body")
		return nil, "", false
	}
	if len(body) > maxRecvMsgBytes {
		p.refuseHTTP(w, r, signal, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		return nil, "", false
	}
	return body, format, true
}

// decodeRequest unmarshals a request body in the encoding the client used.
func decodeRequest(body []byte, format string, msg proto.Message) error {
	if format == contentFormatJSON {
		return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(body, msg)
	}
	return proto.Unmarshal(body, msg)
}

// encodeResponse marshals a response in the same encoding as the request,
// returning the body and its Content-Type.
func encodeResponse(format string, msg proto.Message) ([]byte, string, error) {
	if format == contentFormatJSON {
		out, err := protojson.MarshalOptions{}.Marshal(msg)
		return out, "application/json", err
	}
	out, err := proto.Marshal(msg)
	return out, "application/x-protobuf", err
}

// handleMetrics decodes an OTLP/HTTP metrics request (protobuf or JSON),
// ingests the datapoints, and replies with a (possibly partial-success)
// ExportMetricsServiceResponse in the request's encoding.
func (p *OTLPReceiverProbe) handleMetrics(w http.ResponseWriter, r *http.Request) {
	body, format, ok := p.authorizeAndReadBody(w, r, signalMetrics)
	if !ok {
		return
	}

	var req collectormetricspb.ExportMetricsServiceRequest
	if err := decodeRequest(body, format, &req); err != nil {
		p.refuseHTTP(w, r, signalMetrics, http.StatusBadRequest, "bad_body", "invalid OTLP payload")
		return
	}

	p.stampMetrics(r.RemoteAddr, req.GetResourceMetrics())
	points, dropped := flattenResourceMetrics(req.GetResourceMetrics())
	p.publishMetricBatch(req.GetResourceMetrics())
	if ingestErr := p.ingest(points, dropped); ingestErr != nil {
		http.Error(w, "failed to ingest datapoints", http.StatusInternalServerError)
		return
	}

	resp := &collectormetricspb.ExportMetricsServiceResponse{}
	if dropped > 0 {
		resp.PartialSuccess = &collectormetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: int64(dropped),
			ErrorMessage:       "unrecognized or unset OTLP metric data type not ingested by senhub-agent",
		}
	}

	out, contentType, err := encodeResponse(format, resp)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// handleLogs decodes an OTLP/HTTP logs request (protobuf or JSON),
// publishes the records on the agent log channel for relay, and replies
// with an empty ExportLogsServiceResponse in the request's encoding.
func (p *OTLPReceiverProbe) handleLogs(w http.ResponseWriter, r *http.Request) {
	body, format, ok := p.authorizeAndReadBody(w, r, signalLogs)
	if !ok {
		return
	}

	var req collectorlogspb.ExportLogsServiceRequest
	if err := decodeRequest(body, format, &req); err != nil {
		p.refuseHTTP(w, r, signalLogs, http.StatusBadRequest, "bad_body", "invalid OTLP payload")
		return
	}

	p.stampLogs(r.RemoteAddr, req.GetResourceLogs())
	p.ingestLogs(req.GetResourceLogs())

	out, contentType, err := encodeResponse(format, &collectorlogspb.ExportLogsServiceResponse{})
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// handleTraces decodes an OTLP/HTTP traces request (protobuf or JSON),
// publishes the ResourceSpans verbatim on the agent span channel for
// relay, and replies with an empty ExportTraceServiceResponse in the
// request's encoding.
func (p *OTLPReceiverProbe) handleTraces(w http.ResponseWriter, r *http.Request) {
	body, format, ok := p.authorizeAndReadBody(w, r, signalTraces)
	if !ok {
		return
	}

	var req collectortracepb.ExportTraceServiceRequest
	if err := decodeRequest(body, format, &req); err != nil {
		p.refuseHTTP(w, r, signalTraces, http.StatusBadRequest, "bad_body", "invalid OTLP payload")
		return
	}

	p.stampSpans(r.RemoteAddr, req.GetResourceSpans())
	p.ingestSpans(req.GetResourceSpans())

	out, contentType, err := encodeResponse(format, &collectortracepb.ExportTraceServiceResponse{})
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

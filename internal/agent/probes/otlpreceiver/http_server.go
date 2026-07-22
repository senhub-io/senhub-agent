package otlpreceiver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
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
	lis, err := net.Listen("tcp", p.config.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", p.config.Address, err)
	}

	mux := http.NewServeMux()
	if p.config.Signals.Metrics {
		mux.HandleFunc(p.config.HTTPPath, p.handleMetrics)
	}
	if p.config.Signals.Logs {
		mux.HandleFunc(httpLogsPath, p.handleLogs)
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

	go func() {
		if serveErr := server.Serve(lis); serveErr != nil && serveErr != http.ErrServerClosed {
			p.moduleLogger.Error().Err(serveErr).Msg("OTLP HTTP server stopped with error")
		}
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
		Str("path", p.config.HTTPPath).
		Strs("signals", p.config.Signals.names()).
		Msg("OTLP HTTP receiver started")
	return nil
}

// authorizeAndReadBody runs the shared ingress preamble for an OTLP/HTTP
// handler: guard (bearer/CIDR/rate-limit), POST-only, protobuf content type,
// and a size-capped body read. It writes the error response itself and
// returns ok=false when the request must not proceed.
func (p *OTLPReceiverProbe) authorizeAndReadBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if err := p.guard.allow(r.RemoteAddr, r.Header.Get("Authorization")); err != nil {
		p.logRejection(r.RemoteAddr, err)
		code := http.StatusInternalServerError
		switch {
		case errors.Is(err, errUnauthorized):
			code = http.StatusUnauthorized
		case errors.Is(err, errForbidden):
			code = http.StatusForbidden
		case errors.Is(err, errRateLimited):
			code = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), code)
		return nil, false
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "" && contentType != "application/x-protobuf" {
		http.Error(w, "only application/x-protobuf is supported", http.StatusUnsupportedMediaType)
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRecvMsgBytes))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// handleMetrics decodes an OTLP/HTTP protobuf metrics request, ingests
// the datapoints, and replies with a (possibly partial-success) protobuf
// ExportMetricsServiceResponse. Only the protobuf content type is
// accepted — JSON ingestion is intentionally out of scope for this slice.
func (p *OTLPReceiverProbe) handleMetrics(w http.ResponseWriter, r *http.Request) {
	body, ok := p.authorizeAndReadBody(w, r)
	if !ok {
		return
	}

	var req collectormetricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid OTLP protobuf payload", http.StatusBadRequest)
		return
	}

	points, dropped := flattenResourceMetrics(req.GetResourceMetrics())
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

	out, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// handleLogs decodes an OTLP/HTTP protobuf logs request, publishes the
// records on the agent log channel for relay, and replies with an empty
// ExportLogsServiceResponse. Same protobuf-only, guarded contract as
// handleMetrics.
func (p *OTLPReceiverProbe) handleLogs(w http.ResponseWriter, r *http.Request) {
	body, ok := p.authorizeAndReadBody(w, r)
	if !ok {
		return
	}

	var req collectorlogspb.ExportLogsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid OTLP protobuf payload", http.StatusBadRequest)
		return
	}

	p.ingestLogs(flattenResourceLogs(req.GetResourceLogs(), p.GetName()))

	out, err := proto.Marshal(&collectorlogspb.ExportLogsServiceResponse{})
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

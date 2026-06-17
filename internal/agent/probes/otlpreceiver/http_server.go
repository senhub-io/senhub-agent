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
	mux.HandleFunc(p.config.HTTPPath, p.handleMetrics)

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
		Msg("OTLP HTTP receiver started")
	return nil
}

// handleMetrics decodes an OTLP/HTTP protobuf metrics request, ingests
// the datapoints, and replies with a (possibly partial-success) protobuf
// ExportMetricsServiceResponse. Only the protobuf content type is
// accepted — JSON ingestion is intentionally out of scope for this slice.
func (p *OTLPReceiverProbe) handleMetrics(w http.ResponseWriter, r *http.Request) {
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
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "" && contentType != "application/x-protobuf" {
		http.Error(w, "only application/x-protobuf is supported", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRecvMsgBytes))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
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
			ErrorMessage:       "non-scalar metric types (histogram/summary) are not ingested by senhub-agent",
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

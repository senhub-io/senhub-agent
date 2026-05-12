// senhub-agent/internal/agent/services/data_store/strategies/http/http_tracing.go
//
// Middleware that emits an OTel SERVER span for every served request.
// Attributes follow the OTel HTTP semantic conventions
// (https://opentelemetry.io/docs/specs/semconv/http/http-spans/).
//
// Cardinality is bounded by the gorilla/mux route templates (same trick
// the request counter middleware uses): span name is "{METHOD}
// {route_template}", not the full URL with path params.
package http

import (
	"net/http"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// httpServerScope is the OTel instrumentation scope name for HTTP
// server spans emitted by this middleware.
const httpServerScope = "senhub-agent/http-server"

// TraceRequests is a gorilla/mux middleware that wraps each request in
// an OTel SERVER span. Place AFTER CountRequests in the middleware
// chain so the counter increments even if the tracer panics for any
// reason — counters are core observability, tracing is best-effort.
//
// When traces are disabled, otel.Tracer returns the global noop tracer
// and the span calls compile down to near-zero overhead (one
// noopSpan allocation).
func TraceRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := "_unmatched"
		if cr := mux.CurrentRoute(r); cr != nil {
			if tmpl, err := cr.GetPathTemplate(); err == nil {
				route = tmpl
			}
		}

		tracer := otel.Tracer(httpServerScope)
		ctx, span := tracer.Start(r.Context(), r.Method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", route),
			attribute.String("url.path", r.URL.Path),
			attribute.String("network.peer.address", r.RemoteAddr),
			attribute.String("server.address", r.Host),
		)
		defer span.End()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.response.status_code", rec.status))
		// Per OTel HTTP semconv: 5xx → Error, 4xx is left Unset by
		// convention (client-induced, not a server-side failure).
		if rec.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
	})
}

// statusRecorder wraps http.ResponseWriter to capture the status code
// the handler wrote. http.ResponseWriter has no public accessor for
// the response status — the canonical solution is a small wrapper
// that intercepts WriteHeader. Defaults to 200 because handlers that
// never call WriteHeader explicitly return 200 OK on the first Write.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	// Body Write triggers an implicit 200 — record that so the span
	// reflects what the client actually sees.
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

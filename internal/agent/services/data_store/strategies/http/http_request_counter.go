// senhub-agent/internal/agent/services/data_store/strategies/http/http_request_counter.go
//
// Middleware that counts HTTP requests served per route pattern. Read by
// the Prometheus bridge to expose senhub_agent_http_requests_total{endpoint}.
//
// Counters are keyed by mux route template (e.g. "/api/{agentkey}/prtg/metrics"),
// not by full URL — so the cardinality stays bounded by the number of
// registered endpoints (~20), not by the number of unique agent keys.
package http

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/mux"
)

// httpRequestCounters maps route template → atomic counter.
// Package-level to be shared across handler invocations and accessible
// by the Prometheus bridge without weaving state through structs.
var (
	httpRequestCountersMu sync.RWMutex
	httpRequestCounters   = map[string]*atomic.Uint64{}
)

// CountRequests is a gorilla/mux middleware that increments the counter
// for the matched route on every served request. Place at the top of the
// middleware chain (router.Use) so it observes ALL handlers regardless of
// auth outcome.
func CountRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Resolve the route template the request matched (e.g.
		// "/api/{agentkey}/prtg/metrics"). Falls back to the raw URL path
		// when no route matches (404), so we still count those.
		endpoint := r.URL.Path
		if route := mux.CurrentRoute(r); route != nil {
			if tmpl, err := route.GetPathTemplate(); err == nil {
				endpoint = tmpl
			}
		}
		incrementRequestCounter(endpoint)
		next.ServeHTTP(w, r)
	})
}

func incrementRequestCounter(endpoint string) {
	httpRequestCountersMu.RLock()
	c, ok := httpRequestCounters[endpoint]
	httpRequestCountersMu.RUnlock()
	if !ok {
		httpRequestCountersMu.Lock()
		// Re-check under write lock to avoid duplicate creation under race.
		c, ok = httpRequestCounters[endpoint]
		if !ok {
			c = new(atomic.Uint64)
			httpRequestCounters[endpoint] = c
		}
		httpRequestCountersMu.Unlock()
	}
	c.Add(1)
}

// GetHTTPRequestCounts returns a snapshot of (endpoint → total requests
// served since process start). Read by the Prometheus bridge.
func GetHTTPRequestCounts() map[string]uint64 {
	httpRequestCountersMu.RLock()
	defer httpRequestCountersMu.RUnlock()
	out := make(map[string]uint64, len(httpRequestCounters))
	for k, v := range httpRequestCounters {
		out[k] = v.Load()
	}
	return out
}

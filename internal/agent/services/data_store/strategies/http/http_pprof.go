// Package http — runtime profiling endpoints.
//
// `net/http/pprof` exposes goroutine stacks, heap profiles, CPU
// samples and execution traces under `/debug/pprof/`. We mount the
// same handlers under `/api/{agentkey}/debug/pprof/` so the agentkey
// authentication that protects every other `/api/*` route also
// gates the profiling surface. Operators can then do:
//
//	# print every goroutine's stack — needed for the bbcloud
//	# "agent silent after JT400 respawn" stall investigation
//	curl http://localhost:8080/api/$KEY/debug/pprof/goroutine?debug=2
//
//	# 30-second CPU sample
//	curl -o cpu.pprof http://localhost:8080/api/$KEY/debug/pprof/profile?seconds=30
//
//	# heap snapshot
//	curl -o heap.pprof http://localhost:8080/api/$KEY/debug/pprof/heap
//
// The endpoints are mounted on the same router as the rest of the
// HTTP strategy, which already binds to whatever `bind_address` the
// operator configured (defaults to all interfaces; production
// deployments should pin to 127.0.0.1 — same advice as for the rest
// of the HTTP API).
package http

import (
	"net/http"
	"net/http/pprof"

	"github.com/gorilla/mux"
)

// registerPprofRoutes wires net/http/pprof's stdlib handlers behind
// the agentkey auth check, mirroring the pattern used for the other
// /api/{agentkey}/debug/* routes.
//
// gorilla/mux + http.DefaultServeMux interplay note: net/http/pprof
// registers its handlers on http.DefaultServeMux via init(); we
// deliberately do NOT mount that mux here, we reference the
// individual handlers directly. This keeps our router clean and
// avoids the well-known gotcha where importing pprof for side
// effects pollutes any router that includes DefaultServeMux.
func registerPprofRoutes(router *mux.Router, h *HTTPHandlers) {
	sub := router.PathPrefix("/api/{agentkey}/debug/pprof").Subrouter()

	// Index lists the available profiles. Handler reads
	// path-trimmed names, so we register both the bare path and
	// "/" form.
	sub.HandleFunc("", h.handlePprofIndex).Methods("GET")
	sub.HandleFunc("/", h.handlePprofIndex).Methods("GET")

	// Profile-specific endpoints. Each is provided by net/http/pprof
	// as a typed handler — we just need to call it after auth.
	sub.HandleFunc("/cmdline", h.handlePprofCmdline).Methods("GET")
	sub.HandleFunc("/profile", h.handlePprofProfile).Methods("GET")
	sub.HandleFunc("/symbol", h.handlePprofSymbol).Methods("GET", "POST")
	sub.HandleFunc("/trace", h.handlePprofTrace).Methods("GET")

	// Named profiles — pprof.Handler(name) dispatches by URL path
	// component, so we forward the gorilla-mux variable.
	sub.HandleFunc("/{name:goroutine|heap|allocs|threadcreate|block|mutex}", h.handlePprofNamed).Methods("GET")
}

// authedPprof gates every pprof handler behind the agentkey check
// the rest of the HTTP API uses. We wrap the stdlib handler at
// call time rather than pre-wrapping at register time so the auth
// behaviour stays consistent with the surrounding handler set.
func (h *HTTPHandlers) authedPprof(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if _, ok := h.strategy.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	next(w, r)
}

func (h *HTTPHandlers) handlePprofIndex(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, pprof.Index)
}
func (h *HTTPHandlers) handlePprofCmdline(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, pprof.Cmdline)
}
func (h *HTTPHandlers) handlePprofProfile(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, pprof.Profile)
}
func (h *HTTPHandlers) handlePprofSymbol(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, pprof.Symbol)
}
func (h *HTTPHandlers) handlePprofTrace(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, pprof.Trace)
}

// handlePprofNamed dispatches to pprof.Handler(name).ServeHTTP for
// the well-known named profiles (goroutine, heap, allocs, block,
// mutex, threadcreate). The regex on the route guards against
// arbitrary names being passed in.
func (h *HTTPHandlers) handlePprofNamed(w http.ResponseWriter, r *http.Request) {
	h.authedPprof(w, r, func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["name"]
		pprof.Handler(name).ServeHTTP(w, r)
	})
}

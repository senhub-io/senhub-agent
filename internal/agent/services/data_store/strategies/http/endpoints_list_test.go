package http

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
)

func TestRegisteredEndpoints_ReadsTheRouter(t *testing.T) {
	r := mux.NewRouter()
	noop := func(http.ResponseWriter, *http.Request) {}
	r.HandleFunc("/health", noop).Methods("GET")
	r.HandleFunc("/api/{agentkey}/debug/logs", noop).Methods("GET")
	r.HandleFunc("/api/{agentkey}/debug/logs", noop).Methods("POST")
	r.HandleFunc("/api/{agentkey}/config/outputs", noop).Methods("GET", "POST")
	r.HandleFunc("/web/{agentkey}/dashboard", noop).Methods("GET")

	got := map[string]EndpointInfo{}
	for _, e := range registeredEndpoints(r) {
		got[e.Path] = e
	}
	if len(got) != 3 {
		t.Fatalf("want 3 API routes (console pages left out), got %v", got)
	}
	if e := got["/api/{agentkey}/debug/logs"]; len(e.Methods) != 2 || e.Category != "admin" || e.Description == "" {
		t.Errorf("a path registered twice is one entry with both methods, filed under admin: %+v", e)
	}
	if e := got["/api/{agentkey}/config/outputs"]; e.Category != "admin" {
		t.Errorf("a route the hand-written list missed is listed and categorised: %+v", e)
	}
	if _, listed := got["/api/{agentkey}/debug/inject-test-metrics"]; listed {
		t.Error("a route that is not registered must not be listed")
	}
}

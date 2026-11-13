package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestTracedPathMasksTheKey(t *testing.T) {
	const key = "s3cr3t-admin-key"
	var got string
	var ok bool
	router := mux.NewRouter()
	router.HandleFunc("/api/{agentkey}/info/probes", func(w http.ResponseWriter, r *http.Request) {
		got, ok = tracedPath(r, "/api/{agentkey}/info/probes")
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/"+key+"/info/probes", nil))
	if !ok || got != "/api/{agentkey}/info/probes" {
		t.Fatalf("tracedPath = %q, %v; want the key masked", got, ok)
	}
}

func TestTracedPathLeavesOutUnmatchedRoutes(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/s3cr3t/unknown", nil)
	if path, ok := tracedPath(r, "_unmatched"); ok {
		t.Fatalf("unmatched route traced its path %q; it may carry a key", path)
	}
}

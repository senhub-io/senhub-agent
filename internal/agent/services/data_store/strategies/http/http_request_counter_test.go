package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestCountRequests_PerRouteTemplate(t *testing.T) {
	// Reset the counter map for test isolation.
	resetHTTPRequestCountersForTest()

	router := mux.NewRouter()
	router.Use(CountRequests)
	router.HandleFunc("/api/{agentkey}/probe/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	// 3 hits on /api/.../probe/cpu, 2 hits on /health
	for i := 0; i < 3; i++ {
		resp, _ := http.Get(srv.URL + "/api/abc/probe/cpu")
		if resp != nil {
			resp.Body.Close()
		}
	}
	for i := 0; i < 2; i++ {
		resp, _ := http.Get(srv.URL + "/health")
		if resp != nil {
			resp.Body.Close()
		}
	}

	counts := GetHTTPRequestCounts()
	if counts["/api/{agentkey}/probe/{name}"] != 3 {
		t.Errorf("probe template count: got %d, want 3 (counts=%v)", counts["/api/{agentkey}/probe/{name}"], counts)
	}
	if counts["/health"] != 2 {
		t.Errorf("/health count: got %d, want 2 (counts=%v)", counts["/health"], counts)
	}
}

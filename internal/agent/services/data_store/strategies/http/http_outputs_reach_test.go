package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The event output posts to <server_url>/event/insert. The test used to
// pass on any HTTP answer, so a wrong server_url answering 404 read as a
// working output.
func TestTheEventOutputTestFailsWhereNothingIsServed(t *testing.T) {
	for _, tc := range []struct {
		status int
		pass   bool
	}{
		{http.StatusOK, true},
		{http.StatusBadRequest, true},
		{http.StatusMethodNotAllowed, true},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, false},
		{http.StatusBadGateway, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(tc.status)
		}))
		steps := probeHTTPTarget(context.Background(), "event", map[string]interface{}{"server_url": srv.URL}, 2*time.Second)
		srv.Close()
		last := steps[len(steps)-1]
		if last.Name != "reach" || last.Passed != tc.pass {
			t.Errorf("HTTP %d: steps = %+v, want reach passed=%v", tc.status, steps, tc.pass)
		}
	}
}

func TestTheEventOutputTestPostsWhereTheIntakeRejectsHead(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	steps := probeHTTPTarget(context.Background(), "event", map[string]interface{}{"server_url": srv.URL}, 2*time.Second)
	last := steps[len(steps)-1]
	if last.Name != "reach" || !last.Passed {
		t.Fatalf("steps = %+v, want reach passed", steps)
	}
	if len(methods) != 1 || methods[0] != http.MethodPost {
		t.Errorf("methods = %v, want a single POST", methods)
	}
}

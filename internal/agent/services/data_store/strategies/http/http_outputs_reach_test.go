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
		{http.StatusMethodNotAllowed, true}, // the route exists; HEAD is just not its method
		{http.StatusUnauthorized, true},
		{http.StatusNotFound, false},
		{http.StatusBadGateway, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

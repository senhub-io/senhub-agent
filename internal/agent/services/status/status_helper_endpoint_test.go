package status

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// An output serving TLS answered nothing to the plain-HTTP request status
// sent, and status reported a running agent as unreachable (#969).
func TestStatusHelper_ReachesATLSOutput(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running","version":"0.6.1","health":{"status":"healthy"}}`))
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	h := NewStatusHelper(logger.NewLogger(&cliArgs.ParsedArgs{}))
	if _, err := h.GetDetailedStatusFromHTTP("k", port); err == nil {
		t.Fatal("plain HTTP to a TLS listener was expected to fail; the test proves nothing otherwise")
	}
	h.SetEndpoint("https", host)
	st, err := h.GetDetailedStatusFromHTTP("k", port)
	if err != nil {
		t.Fatalf("status over TLS: %v", err)
	}
	if st.Agent.Version != "0.6.1" {
		t.Errorf("version = %q", st.Agent.Version)
	}
}

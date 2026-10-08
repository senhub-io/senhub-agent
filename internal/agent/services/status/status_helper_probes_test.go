package status

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func probeServer(t *testing.T, probesBody string) (*StatusHelper, int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/info/probes") {
			_, _ = w.Write([]byte(probesBody))
			return
		}
		_, _ = w.Write([]byte(`{"status":"running","version":"0.6.1","health":{"status":"healthy"}}`))
	}))
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	h := NewStatusHelper(logger.NewLogger(&cliArgs.ParsedArgs{}))
	h.SetEndpoint("http", host)
	return h, port, srv.Close
}

// /info/probes sends names in "probes" and the state in "details"; the
// client decoded objects from "probes", failed, and the failure was
// dropped, so status and doctor saw no probe at all.
func TestStatusHelper_ListsProbesFromTheRealPayload(t *testing.T) {
	h, port, closeFn := probeServer(t, `{"probes":["filetail_a","memory"],"probe_metrics":{"filetail_a":0,"memory":12},"total_metrics":12,
"details":[{"name":"filetail_a","metrics_count":0,"running":true,"health":"failed","last_error":"cannot be read: no such file"},
{"name":"memory","metrics_count":12,"last_update":"2026-10-01T10:00:00Z","running":true,"health":"ok"}]}`)
	defer closeFn()

	st, err := h.GetDetailedStatusFromHTTP("k", port)
	if err != nil {
		t.Fatal(err)
	}
	if st.ProbesError != "" || len(st.Probes) != 2 {
		t.Fatalf("probes = %+v, error %q", st.Probes, st.ProbesError)
	}
	byName := map[string]ProbeStatus{}
	for _, p := range st.Probes {
		byName[p.Name] = p
	}
	if p := byName["filetail_a"]; p.Status != "error" || !strings.Contains(p.LastError, "no such file") {
		t.Errorf("failing probe = %+v", p)
	}
	if p := byName["memory"]; p.Status != "active" || p.MetricsCount != 12 || p.LastUpdate.IsZero() {
		t.Errorf("healthy probe = %+v", p)
	}
}

func TestStatusHelper_ListsProbesFromAnAgentWithoutDetails(t *testing.T) {
	h, port, closeFn := probeServer(t, `{"probes":["memory"],"probe_metrics":{"memory":3},"total_metrics":3}`)
	defer closeFn()

	st, err := h.GetDetailedStatusFromHTTP("k", port)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Probes) != 1 || st.Probes[0].Name != "memory" || st.Probes[0].MetricsCount != 3 {
		t.Fatalf("probes = %+v", st.Probes)
	}
}

func TestStatusHelper_SurfacesAProbeListItCannotDecode(t *testing.T) {
	h, port, closeFn := probeServer(t, `{"probes":[{"name":"x"}]}`)
	defer closeFn()

	st, err := h.GetDetailedStatusFromHTTP("k", port)
	if err != nil {
		t.Fatal(err)
	}
	if st.ProbesError == "" {
		t.Error("a probe list that does not decode must be reported, not dropped")
	}
}

package nginx

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

const sampleStubStatus = `Active connections: 291
server accepts handled requests
 16630948 16630948 31070465
Reading: 6 Writing: 179 Waiting: 106
`

func TestParseStubStatus_Valid(t *testing.T) {
	s, err := parseStubStatus(sampleStubStatus)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tests := []struct {
		name string
		got  int64
		want int64
	}{
		{"activeConnections", s.activeConnections, 291},
		{"accepted", s.accepted, 16630948},
		{"handled", s.handled, 16630948},
		{"requests", s.requests, 31070465},
		{"reading", s.reading, 6},
		{"writing", s.writing, 179},
		{"waiting", s.waiting, 106},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("parseStubStatus %s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestParseStubStatus_TooFewLines(t *testing.T) {
	_, err := parseStubStatus("Active connections: 1\nserver accepts handled requests\n")
	if err == nil {
		t.Fatal("expected error for too-few lines")
	}
}

func TestParseStubStatus_BadActiveLine(t *testing.T) {
	body := "Bogus line\nserver accepts handled requests\n 1 1 1\nReading: 0 Writing: 0 Waiting: 0\n"
	_, err := parseStubStatus(body)
	if err == nil {
		t.Fatal("expected error for bad active-connections line")
	}
}

func TestParseStubStatus_BadCounts(t *testing.T) {
	body := "Active connections: 1\nserver accepts handled requests\n X 1 1\nReading: 0 Writing: 0 Waiting: 0\n"
	_, err := parseStubStatus(body)
	if err == nil {
		t.Fatal("expected error for non-numeric accepts field")
	}
}

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestCollect_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleStubStatus))
	}))
	defer srv.Close()

	probe, err := NewNginxProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected datapoints, got none")
	}

	byName := make(map[string]float64)
	for _, p := range points {
		byName[p.Name] = p.Value
	}

	if byName["senhub.nginx.up"] != 1 {
		t.Errorf("senhub.nginx.up = %v, want 1", byName["senhub.nginx.up"])
	}
	if byName["nginx.connections.current"] != 291 {
		t.Errorf("nginx.connections.current = %v, want 291", byName["nginx.connections.current"])
	}
	if byName["nginx.requests"] != 31070465 {
		t.Errorf("nginx.requests = %v, want 31070465", byName["nginx.requests"])
	}
	if byName["nginx.connections.reading"] != 6 {
		t.Errorf("nginx.connections.reading = %v, want 6", byName["nginx.connections.reading"])
	}
	if byName["nginx.connections.writing"] != 179 {
		t.Errorf("nginx.connections.writing = %v, want 179", byName["nginx.connections.writing"])
	}
	if byName["nginx.connections.waiting"] != 106 {
		t.Errorf("nginx.connections.waiting = %v, want 106", byName["nginx.connections.waiting"])
	}
}

func TestCollect_Down(t *testing.T) {
	// Point at a port that refuses connections
	probe, err := NewNginxProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:1", // port 1 is never open
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error for a down target, got: %v", err)
	}

	byName := make(map[string]float64)
	for _, p := range points {
		byName[p.Name] = p.Value
	}

	if byName["senhub.nginx.up"] != 0 {
		t.Errorf("senhub.nginx.up = %v, want 0 when endpoint unreachable", byName["senhub.nginx.up"])
	}
}

func TestNewNginxProbe_Defaults(t *testing.T) {
	p, err := NewNginxProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}
	np := p.(*NginxProbe)
	if np.cfg.Endpoint != defaultEndpoint {
		t.Errorf("default endpoint = %q, want %q", np.cfg.Endpoint, defaultEndpoint)
	}
	if np.cfg.Interval != defaultInterval {
		t.Errorf("default interval = %v, want %v", np.cfg.Interval, defaultInterval)
	}
}

func TestDownTargetWarnsOncePerOutage(t *testing.T) {
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(sampleStubStatus))
	}))
	defer srv.Close()
	probe, err := NewNginxProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}
	p := probe.(*NginxProbe)

	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.test")

	down.Store(true)
	for i := 0; i < 10; i++ {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect must not return an error for a down target, got: %v", err)
		}
		if got := upValue(points, "senhub.nginx.up"); got != 0 {
			t.Fatalf("%s = %v while the target is down, want 0", "senhub.nginx.up", got)
		}
	}
	down.Store(false)
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := strings.Count(buf.String(), `"level":"warn"`); got != 1 {
		t.Errorf("%d warnings over 10 failed cycles, want 1\n%s", got, buf.String())
	}
	if got := strings.Count(buf.String(), `"level":"info"`); got != 1 {
		t.Errorf("%d recovery lines, want 1\n%s", got, buf.String())
	}
}

func upValue(points []data_store.DataPoint, name string) float64 {
	for _, dp := range points {
		if dp.Name == name {
			return dp.Value
		}
	}
	return -1
}

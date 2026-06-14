package haproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// sampleCSV is a representative HAProxy stats CSV response with one
// frontend row, one backend aggregate row and one server row.
// Columns follow the HAProxy stats CSV v3 layout where type is at
// index 47 (0-indexed) and req_rate is at index 46. Empty fields use
// the comma placeholder to maintain correct column offsets.
const sampleCSV = `# pxname,svname,...(48 columns)
http_frontend,FRONTEND,,,12,,,,5000,1024000,2048000,,5,0,0,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,8,0,
http_backend,BACKEND,,,3,,,,2000,512000,1024000,,0,2,3,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,0,1,
http_backend,web1,,,2,,,,1500,256000,512000,,0,1,2,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,0,2,
`

func TestParseCSV_ValidInput(t *testing.T) {
	r := strings.NewReader(sampleCSV)
	rows, err := parseCSV(r)
	if err != nil {
		t.Fatalf("parseCSV() error: %v", err)
	}
	// 3 data rows (comment header is skipped by csv.Reader.Comment).
	if len(rows) != 3 {
		t.Errorf("parseCSV() returned %d rows, want 3", len(rows))
	}
}

func TestParseCSV_EmptyBody(t *testing.T) {
	r := strings.NewReader("")
	rows, err := parseCSV(r)
	if err != nil {
		t.Fatalf("parseCSV() error on empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("parseCSV() returned %d rows for empty input, want 0", len(rows))
	}
}

func TestBuildDatapoints_Counts(t *testing.T) {
	r := strings.NewReader(sampleCSV)
	rows, _ := parseCSV(r)

	probe := &haproxyProbe{}
	points := probe.buildDatapoints(rows, time.Time{})

	names := make(map[string]int)
	for _, p := range points {
		names[p.Name]++
	}

	// Sessions only for frontend (type=0) and backend aggregate (type=1), not the server row (type=2).
	if names["haproxy.sessions.count"] != 2 {
		t.Errorf("haproxy.sessions.count count = %d, want 2", names["haproxy.sessions.count"])
	}
	// Bytes for all 3 rows.
	if names["haproxy.bytes.input"] != 3 {
		t.Errorf("haproxy.bytes.input count = %d, want 3", names["haproxy.bytes.input"])
	}
	// Request errors for frontend only.
	if names["haproxy.requests.errors"] != 1 {
		t.Errorf("haproxy.requests.errors count = %d, want 1", names["haproxy.requests.errors"])
	}
}

func TestBuildDatapoints_ProxyTag(t *testing.T) {
	r := strings.NewReader(sampleCSV)
	rows, _ := parseCSV(r)

	probe := &haproxyProbe{}
	points := probe.buildDatapoints(rows, time.Time{})

	for _, p := range points {
		var proxy string
		for _, tg := range p.Tags {
			if tg.Key == "proxy" {
				proxy = tg.Value
			}
		}
		if proxy == "" {
			t.Errorf("metric %q missing proxy tag", p.Name)
		}
	}
}

func TestCollect_Up_WhenServerReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	probe := makeTestProbe(srv.URL)
	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	upVal := findMetricValue(points, "senhub.haproxy.up")
	if upVal == nil {
		t.Fatal("senhub.haproxy.up metric not emitted")
	}
	if *upVal != 1 {
		t.Errorf("senhub.haproxy.up = %v, want 1", *upVal)
	}
}

func TestCollect_Down_WhenServerUnreachable(t *testing.T) {
	probe := makeTestProbe("http://127.0.0.1:19999/stats;csv")
	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() should not propagate errors, got: %v", err)
	}
	upVal := findMetricValue(points, "senhub.haproxy.up")
	if upVal == nil {
		t.Fatal("senhub.haproxy.up metric not emitted on failure")
	}
	if *upVal != 0 {
		t.Errorf("senhub.haproxy.up = %v, want 0 on unreachable target", *upVal)
	}
}

func TestCollect_BasicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	probe := makeTestProbe(srv.URL)
	probe.cfg.Username = "admin"
	probe.cfg.Password = "secret"

	_, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if gotUser != "admin" || gotPass != "secret" {
		t.Errorf("BasicAuth not set: got user=%q pass=%q", gotUser, gotPass)
	}
}

func TestComponentName(t *testing.T) {
	tests := []struct {
		rowType int
		want    string
	}{
		{typeFrontend, "frontend"},
		{typeBackend, "backend"},
		{typeServer, "server"},
		{99, "other"},
	}
	for _, tt := range tests {
		got := componentName(tt.rowType)
		if got != tt.want {
			t.Errorf("componentName(%d) = %q, want %q", tt.rowType, got, tt.want)
		}
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"123", 123},
		{"0", 0},
		{"", 0},
		{" 42 ", 42},
		{"abc", 0},
	}
	for _, tt := range tests {
		got := parseInt(tt.in)
		if got != tt.want {
			t.Errorf("parseInt(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func makeTestProbe(endpoint string) *haproxyProbe {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.haproxy.test")
	addr, port := endpointHostPort(endpoint)
	p := &haproxyProbe{
		BaseProbe: &types.BaseProbe{},
		cfg: haproxyConfig{
			Endpoint: endpoint,
			Timeout:  2 * time.Second,
			Interval: 60 * time.Second,
		},
		moduleLogger: moduleLogger,
		client:       &http.Client{Timeout: 2 * time.Second},
		entitySrc:    newHAProxyEntitySource(addr, port),
	}
	p.SetProbeType(ProbeType)
	return p
}

func findMetricValue(points []data_store.DataPoint, name string) *float32 {
	for _, p := range points {
		if p.Name == name {
			v := p.Value
			return &v
		}
	}
	return nil
}

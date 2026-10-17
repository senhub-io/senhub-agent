package haproxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

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
		entitySrc:    newHAProxyEntitySource(addr, port, "", "test-host-id"),
	}
	p.SetProbeType(ProbeType)
	return p
}

func findMetricValue(points []data_store.DataPoint, name string) *float64 {
	for _, p := range points {
		if p.Name == name {
			v := p.Value
			return &v
		}
	}
	return nil
}

// The HTML stats page answers on the URL without ";csv". Read as CSV it
// failed on "bare \" in non-quoted-field"; the error now names the fix.
func TestFetchCSV_HTMLPageNamesTheCSVForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body class="x">stats</body></html>`))
	}))
	defer srv.Close()

	probe := makeTestProbe(srv.URL + "/stats")
	_, err := probe.fetchCSV()
	if err == nil || !strings.Contains(err.Error(), "/stats;csv") {
		t.Errorf("error = %v, want it to point at the ;csv form", err)
	}
}

func TestDownTargetWarnsOncePerOutage(t *testing.T) {
	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()
	p := makeTestProbe(srv.URL)

	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.test")

	down.Store(true)
	for i := 0; i < 10; i++ {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect must not return an error for a down target, got: %v", err)
		}
		if got := metricOrMissing(points, "senhub.haproxy.up"); got != 0 {
			t.Fatalf("%s = %v while the target is down, want 0", "senhub.haproxy.up", got)
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

func metricOrMissing(points []data_store.DataPoint, name string) float64 {
	if v := findMetricValue(points, name); v != nil {
		return *v
	}
	return -1
}

// HAProxy documents econ and eresp as [..BS]: a frontend row has no value for
// them and the CSV leaves the cell empty. Emitting 0 there reports "no
// connection errors" for a measurement that does not exist.
func TestBuildDatapoints_EmptyCellsAreNotZero(t *testing.T) {
	row := func(name, svname, rowType string, cells map[int]string) []string {
		r := make([]string, colType+1)
		r[colPxname], r[colSvname], r[colType] = name, svname, rowType
		for i, v := range cells {
			r[i] = v
		}
		return r
	}
	rows := [][]string{
		row("fe", "FRONTEND", "0", map[int]string{colScur: "12", colStot: "40", colBin: "5000", colBout: "1024", colEreq: "5", colReqRate: "3"}),
		row("be", "BACKEND", "1", map[int]string{colScur: "3", colStot: "20", colBin: "2000", colBout: "512", colEcon: "2", colEresp: "1"}),
	}
	points := (&haproxyProbe{}).buildDatapoints(rows, time.Time{})

	byProxy := func(proxy, name string) *float64 {
		for _, p := range points {
			if p.Name != name {
				continue
			}
			for _, tg := range p.Tags {
				if tg.Key == "proxy" && tg.Value == proxy {
					v := p.Value
					return &v
				}
			}
		}
		return nil
	}

	for _, name := range []string{"haproxy.connections.errors", "haproxy.responses.errors"} {
		if v := byProxy("fe", name); v != nil {
			t.Errorf("%s emitted as %v for a frontend whose cell is empty", name, *v)
		}
	}
	if v := byProxy("be", "haproxy.connections.errors"); v == nil || *v != 2 {
		t.Errorf("backend haproxy.connections.errors = %v, want 2", v)
	}
	if v := byProxy("be", "haproxy.responses.errors"); v == nil || *v != 1 {
		t.Errorf("backend haproxy.responses.errors = %v, want 1", v)
	}
	if v := byProxy("fe", "haproxy.requests.errors"); v == nil || *v != 5 {
		t.Errorf("frontend haproxy.requests.errors = %v, want 5", v)
	}
	if v := byProxy("fe", "haproxy.requests.rate"); v == nil || *v != 3 {
		t.Errorf("frontend haproxy.requests.rate = %v, want 3", v)
	}
}

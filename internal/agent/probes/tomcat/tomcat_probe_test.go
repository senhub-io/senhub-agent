package tomcat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// jolokiaServer is a minimal fake Jolokia endpoint for unit tests.
// It maps "<mbean>/<attribute>" to a fixed JSON response value.
type jolokiaServer struct {
	responses map[string]interface{}
}

func (s *jolokiaServer) handler(w http.ResponseWriter, r *http.Request) {
	// URL pattern: /<prefix>/read/<mbean>/<attribute>
	// Find the "/read/" segment and take everything after it as the key.
	path := r.URL.Path
	const readMarker = "/read/"
	idx := strings.Index(path, readMarker)
	if idx < 0 {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	key := path[idx+len(readMarker):]

	val, ok := s.responses[key]
	if !ok {
		// Return a Jolokia-style 404 so the probe can skip unknown MBeans.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": 404,
			"error":  "javax.management.InstanceNotFoundException",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": 200,
		"value":  val,
	})
}

func newTestServer(responses map[string]interface{}) *httptest.Server {
	srv := &jolokiaServer{responses: responses}
	return httptest.NewServer(http.HandlerFunc(srv.handler))
}

func TestNewTomcatProbe_Defaults(t *testing.T) {
	probe, err := NewTomcatProbe(map[string]interface{}{}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewTomcatProbe: %v", err)
	}
	p, ok := probe.(*TomcatProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	if p.cfg.JolokiaURL != defaultJolokiaURL {
		t.Errorf("default jolokia_url = %q, want %q", p.cfg.JolokiaURL, defaultJolokiaURL)
	}
	if p.cfg.Interval != defaultInterval {
		t.Errorf("default interval = %v, want %v", p.cfg.Interval, defaultInterval)
	}
	if p.GetProbeType() != ProbeType {
		t.Errorf("probe type = %q, want %q", p.GetProbeType(), ProbeType)
	}
}

func TestNewTomcatProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"jolokia_url": "http://myhost:9090/jolokia/",
		"username":    "admin",
		"password":    "secret",
		"timeout":     30,
		"interval":    120,
	}
	probe, err := NewTomcatProbe(cfg, testBaseLogger())
	if err != nil {
		t.Fatalf("NewTomcatProbe: %v", err)
	}
	p := probe.(*TomcatProbe)
	// Trailing slash must be stripped.
	if p.cfg.JolokiaURL != "http://myhost:9090/jolokia" {
		t.Errorf("jolokia_url = %q, want trailing slash stripped", p.cfg.JolokiaURL)
	}
	if p.cfg.Username != "admin" {
		t.Errorf("username = %q, want admin", p.cfg.Username)
	}
}

func TestCollect_TomcatUp_WhenJolokiaHealthy(t *testing.T) {
	srv := newTestServer(map[string]interface{}{
		"java.lang:type=Threading/ThreadCount":    float64(42),
		"java.lang:type=Threading/DaemonThreadCount": float64(10),
		"java.lang:type=Memory/HeapMemoryUsage": map[string]interface{}{
			"used":      float64(100 * 1024 * 1024),
			"committed": float64(200 * 1024 * 1024),
			"max":       float64(512 * 1024 * 1024),
		},
	})
	defer srv.Close()

	probe, _ := NewTomcatProbe(map[string]interface{}{"jolokia_url": srv.URL + "/jolokia"}, testBaseLogger())
	p := probe.(*TomcatProbe)
	p.SetName("tomcat-test")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.tomcat.up"] != 1 {
		t.Errorf("senhub.tomcat.up = %v, want 1", byName["senhub.tomcat.up"])
	}

	if byName["jvm.threads.count"] != 42 {
		t.Errorf("jvm.threads.count = %v, want 42", byName["jvm.threads.count"])
	}

	if byName["jvm.memory.heap.used"] != float32(100*1024*1024) {
		t.Errorf("jvm.memory.heap.used = %v, want %v", byName["jvm.memory.heap.used"], float32(100*1024*1024))
	}
}

func TestCollect_TomcatUp_WhenJolokiaDown(t *testing.T) {
	// Use a server that is closed immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	probe, _ := NewTomcatProbe(map[string]interface{}{"jolokia_url": srv.URL + "/jolokia"}, testBaseLogger())
	p := probe.(*TomcatProbe)
	p.SetName("tomcat-down")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect should return at least senhub.tomcat.up=0")
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}
	if byName["senhub.tomcat.up"] != 0 {
		t.Errorf("senhub.tomcat.up = %v, want 0 when Jolokia is down", byName["senhub.tomcat.up"])
	}
}

func TestCollect_ConnectorMetrics(t *testing.T) {
	key8080 := "Catalina:type=GlobalRequestProcessor,name=\"http-nio-8080\""
	srv := newTestServer(map[string]interface{}{
		"java.lang:type=Threading/ThreadCount": float64(50),
		key8080 + "/requestCount":              float64(1000),
		key8080 + "/bytesReceived":             float64(512000),
		key8080 + "/bytesSent":                 float64(2048000),
		key8080 + "/processingTime":            float64(3500),
		key8080 + "/errorCount":                float64(5),
	})
	defer srv.Close()

	probe, _ := NewTomcatProbe(map[string]interface{}{"jolokia_url": srv.URL + "/jolokia"}, testBaseLogger())
	p := probe.(*TomcatProbe)
	p.SetName("tomcat-connectors")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	found := map[string]bool{}
	for _, dp := range points {
		found[dp.Name] = true
	}

	for _, name := range []string{
		"tomcat.requests.total",
		"tomcat.bytes.received",
		"tomcat.bytes.sent",
		"tomcat.processing_time",
		"tomcat.errors.total",
	} {
		if !found[name] {
			t.Errorf("expected metric %q not found in output", name)
		}
	}
}

func TestCollect_AllMetricsHaveProbeName(t *testing.T) {
	srv := newTestServer(map[string]interface{}{
		"java.lang:type=Threading/ThreadCount": float64(20),
	})
	defer srv.Close()

	probe, _ := NewTomcatProbe(map[string]interface{}{"jolokia_url": srv.URL + "/jolokia"}, testBaseLogger())
	p := probe.(*TomcatProbe)
	p.SetName("my-tomcat")

	points, _ := p.Collect()
	for _, dp := range points {
		hasProbeName := false
		for _, tg := range dp.Tags {
			if tg.Key == "probe_name" && tg.Value == "my-tomcat" {
				hasProbeName = true
			}
		}
		if !hasProbeName {
			t.Errorf("datapoint %q missing probe_name tag", dp.Name)
		}
	}
}

func TestExtractContextFromManagerMBean(t *testing.T) {
	tests := []struct {
		mbean string
		want  string
	}{
		{"Catalina:context=/myapp,host=localhost,type=Manager", "/myapp"},
		{"Catalina:context=/,host=localhost,type=Manager", "/"},
		{"no-context-here", "no-context-here"},
	}
	for _, tt := range tests {
		got := extractContextFromManagerMBean(tt.mbean)
		if got != tt.want {
			t.Errorf("extractContextFromManagerMBean(%q) = %q, want %q", tt.mbean, got, tt.want)
		}
	}
}

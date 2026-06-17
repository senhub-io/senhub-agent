package couchdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

// minimalStats returns a minimal valid CouchDB stats payload.
func minimalStats() statsResponse {
	var s statsResponse
	s.HTTPD.Requests.Value = 100
	s.HTTPDRequestMethods.GET.Value = 80
	s.HTTPDRequestMethods.POST.Value = 10
	s.HTTPDRequestMethods.PUT.Value = 8
	s.HTTPDRequestMethods.DELETE.Value = 2
	s.HTTPDStatusCodes.S200.Value = 90
	s.HTTPDStatusCodes.S201.Value = 8
	s.HTTPDStatusCodes.S400.Value = 1
	s.HTTPDStatusCodes.S401.Value = 0
	s.HTTPDStatusCodes.S404.Value = 1
	s.HTTPDStatusCodes.S500.Value = 0
	s.OpenDatabases.Value = 5
	s.OpenOSFiles.Value = 12
	s.DatabaseReads.Value = 200
	s.DatabaseWrites.Value = 50
	s.IOInput.Value = 1024
	s.IOOutput.Value = 2048
	return s
}

func TestNewCouchDBProbe_Defaults(t *testing.T) {
	p, err := NewCouchDBProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe() error = %v", err)
	}
	cp := p.(*CouchDBProbe)
	if cp.cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", cp.cfg.Endpoint, defaultEndpoint)
	}
	if cp.cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", cp.cfg.Interval, defaultInterval)
	}
	if cp.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", cp.cfg.Timeout, defaultTimeout)
	}
}

func TestNewCouchDBProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"endpoint": "http://couch.example.com:5984",
		"username": "admin",
		"password": "secret",
		"timeout":  30,
		"interval": 120,
	}
	p, err := NewCouchDBProbe(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe() error = %v", err)
	}
	cp := p.(*CouchDBProbe)
	if cp.cfg.Endpoint != "http://couch.example.com:5984" {
		t.Errorf("endpoint = %q", cp.cfg.Endpoint)
	}
	if cp.cfg.Username != "admin" {
		t.Errorf("username = %q", cp.cfg.Username)
	}
	if cp.cfg.Timeout != 30*time.Second {
		t.Errorf("timeout = %v", cp.cfg.Timeout)
	}
	if cp.cfg.Interval != 120*time.Second {
		t.Errorf("interval = %v", cp.cfg.Interval)
	}
}

func TestCollect_Success(t *testing.T) {
	stats := minimalStats()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_node/_local/_stats":
			_ = json.NewEncoder(w).Encode(stats)
		case "/":
			// Serve the CouchDB root response with a stable server UUID.
			_ = json.NewEncoder(w).Encode(map[string]string{
				"uuid":    "abc123def456",
				"version": "3.3.2",
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p, err := NewCouchDBProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	// Map by name for assertions.
	byName := map[string][]float64{}
	for _, dp := range points {
		byName[dp.Name] = append(byName[dp.Name], dp.Value)
	}

	// senhub.couchdb.up must be 1.
	upVals, ok := byName["senhub.couchdb.up"]
	if !ok || len(upVals) == 0 {
		t.Error("senhub.couchdb.up missing")
	} else if upVals[0] != 1 {
		t.Errorf("senhub.couchdb.up = %v, want 1", upVals[0])
	}

	// 4 method variants.
	if len(byName["couchdb.httpd.method.requests"]) != 4 {
		t.Errorf("expected 4 method datapoints, got %d", len(byName["couchdb.httpd.method.requests"]))
	}

	// 6 status code variants.
	if len(byName["couchdb.httpd.status.responses"]) != 6 {
		t.Errorf("expected 6 status datapoints, got %d", len(byName["couchdb.httpd.status.responses"]))
	}

	// I/O bytes present.
	if _, ok := byName["couchdb.io.bytes.read"]; !ok {
		t.Error("couchdb.io.bytes.read missing")
	}
	if _, ok := byName["couchdb.io.bytes.written"]; !ok {
		t.Error("couchdb.io.bytes.written missing")
	}
}

func TestCollect_Unreachable(t *testing.T) {
	// Point at a port that refuses connections.
	p, err := NewCouchDBProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:1", // port 1 always refuses
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error on unreachable endpoint, got: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints on unreachable endpoint")
	}

	for _, dp := range points {
		if dp.Name == "senhub.couchdb.up" && dp.Value != 0 {
			t.Errorf("senhub.couchdb.up = %v on unreachable endpoint, want 0", dp.Value)
		}
	}
}

func TestCollect_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, pw, ok := r.BasicAuth()
		if !ok || u != "admin" || pw != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_ = json.NewEncoder(w).Encode(map[string]string{"uuid": "auth-uuid-01", "version": "3.3.2"})
		default:
			_ = json.NewEncoder(w).Encode(minimalStats())
		}
	}))
	defer srv.Close()

	p, err := NewCouchDBProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "admin",
		"password": "pass",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.couchdb.up" {
			if dp.Value != 1 {
				t.Errorf("senhub.couchdb.up = %v with valid auth, want 1", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.couchdb.up datapoint not found")
}

func TestCollect_BadStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, err := NewCouchDBProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should return nil error on bad status, got: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.couchdb.up" && dp.Value != 0 {
			t.Errorf("senhub.couchdb.up = %v on bad status, want 0", dp.Value)
		}
	}
}

func TestBuildDatapoints_MetricCount(t *testing.T) {
	probe, err := NewCouchDBProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}
	cp := probe.(*CouchDBProbe)
	pts := cp.buildDatapoints(&statsResponse{}, time.Now())

	// Expected: 1 up + 1 total requests + 4 methods + 6 status codes +
	//           2 db gauges + 2 db counters + 2 io counters = 18
	if len(pts) != 18 {
		t.Errorf("buildDatapoints returned %d points, want 18", len(pts))
	}
}

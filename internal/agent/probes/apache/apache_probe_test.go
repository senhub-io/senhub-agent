package apache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

const sampleModStatus = `Total Accesses: 1234
Total kBytes: 5678
Uptime: 3600
BusyWorkers: 3
IdleWorkers: 47
ConnsTotal: 50
Scoreboard: ___W_____________________________...
`

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// TestNewApacheProbe_Defaults verifies defaults are applied when no
// configuration keys are provided.
func TestNewApacheProbe_Defaults(t *testing.T) {
	p, err := NewApacheProbe(map[string]interface{}{}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	if ap.cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", ap.cfg.Endpoint, defaultEndpoint)
	}
	if ap.cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", ap.cfg.Interval, defaultInterval)
	}
	if ap.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", ap.cfg.Timeout, defaultTimeout)
	}
}

// TestNewApacheProbe_CustomConfig verifies that all config fields are
// applied correctly.
func TestNewApacheProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"endpoint": "http://apache.local/server-status?auto",
		"username": "admin",
		"password": "secret",
		"interval": 30,
		"timeout":  5,
	}
	p, err := NewApacheProbe(cfg, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	if ap.cfg.Endpoint != "http://apache.local/server-status?auto" {
		t.Errorf("endpoint = %q", ap.cfg.Endpoint)
	}
	if ap.cfg.Username != "admin" {
		t.Errorf("username = %q", ap.cfg.Username)
	}
}

// TestCollect_Success exercises the full happy path against a local
// httptest server serving a valid mod_status body.
func TestCollect_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleModStatus)
	}))
	defer srv.Close()

	p, err := NewApacheProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	byName := map[string][]float32{}
	for _, dp := range points {
		byName[dp.Name] = append(byName[dp.Name], dp.Value)
	}

	// up must be 1
	if v := byName["senhub.apache.up"]; len(v) == 0 || v[0] != 1 {
		t.Errorf("senhub.apache.up = %v, want [1]", v)
	}
	// uptime = 3600 s
	if v := byName["apache.uptime"]; len(v) == 0 || v[0] != 3600 {
		t.Errorf("apache.uptime = %v, want [3600]", v)
	}
	// connections = 50
	if v := byName["apache.current_connections"]; len(v) == 0 || v[0] != 50 {
		t.Errorf("apache.current_connections = %v, want [50]", v)
	}
	// workers: two datapoints (busy + idle)
	if v := byName["apache.workers"]; len(v) != 2 {
		t.Errorf("apache.workers count = %d, want 2 (busy+idle)", len(v))
	}
	// requests = 1234
	if v := byName["apache.requests"]; len(v) == 0 || v[0] != 1234 {
		t.Errorf("apache.requests = %v, want [1234]", v)
	}
	// traffic = 5678 * 1024 bytes
	want := float32(5678 * 1024)
	if v := byName["apache.traffic.bytes"]; len(v) == 0 || v[0] != want {
		t.Errorf("apache.traffic.bytes = %v, want [%v]", v, want)
	}
}

// TestCollect_Up0OnError verifies that a fetch failure produces
// senhub.apache.up=0 but no collection error.
func TestCollect_Up0OnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, err := NewApacheProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error: %v", err)
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.apache.up"] != 0 {
		t.Errorf("senhub.apache.up = %v on error, want 0", byName["senhub.apache.up"])
	}
}

// TestCollect_BasicAuth verifies that credentials are forwarded as an
// Authorization header.
func TestCollect_BasicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		fmt.Fprint(w, sampleModStatus)
	}))
	defer srv.Close()

	p, err := NewApacheProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "user1",
		"password": "pw2",
	}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	if _, err := ap.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if gotUser != "user1" || gotPass != "pw2" {
		t.Errorf("BasicAuth forwarded user=%q pass=%q, want user1/pw2", gotUser, gotPass)
	}
}

// TestCollect_UnreachableServer verifies that a connection failure
// produces senhub.apache.up=0 and no collection error.
func TestCollect_UnreachableServer(t *testing.T) {
	p, err := NewApacheProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999/server-status?auto",
		"timeout":  1,
	}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error on connection failure: %v", err)
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}
	if byName["senhub.apache.up"] != 0 {
		t.Errorf("senhub.apache.up = %v, want 0", byName["senhub.apache.up"])
	}
}

// TestWorkerStateTags verifies that the workers metric carries a "state"
// tag distinguishing busy from idle.
func TestWorkerStateTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleModStatus)
	}))
	defer srv.Close()

	p, err := NewApacheProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	stateValues := map[string]float32{}
	for _, dp := range points {
		if dp.Name != "apache.workers" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "state" {
				stateValues[tg.Value] = dp.Value
			}
		}
	}

	if v, ok := stateValues["busy"]; !ok || v != 3 {
		t.Errorf("workers state=busy = %v, want 3", v)
	}
	if v, ok := stateValues["idle"]; !ok || v != 47 {
		t.Errorf("workers state=idle = %v, want 47", v)
	}
}

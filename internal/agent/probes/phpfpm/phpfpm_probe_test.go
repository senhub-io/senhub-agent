package phpfpm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// newTestLogger returns a minimal logger suitable for unit tests.
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestNewPHPFPMProbe_Defaults(t *testing.T) {
	probe, err := NewPHPFPMProbe(map[string]interface{}{}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}
	if probe == nil {
		t.Fatal("NewPHPFPMProbe() returned nil probe")
	}
	if p, ok := probe.(*phpFPMProbe); ok {
		if p.GetProbeType() != ProbeType {
			t.Errorf("GetProbeType() = %q, want %q", p.GetProbeType(), ProbeType)
		}
	}
	if !probe.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
}

func TestNewPHPFPMProbe_CustomConfig(t *testing.T) {
	config := map[string]interface{}{
		"endpoint": "http://example.com/fpm-status?json",
		"interval": 30,
		"timeout":  5,
	}
	probe, err := NewPHPFPMProbe(config, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}
	p := probe.(*phpFPMProbe)
	if p.cfg.Endpoint != "http://example.com/fpm-status?json" {
		t.Errorf("Endpoint = %q, want http://example.com/fpm-status?json", p.cfg.Endpoint)
	}
	if p.cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", p.cfg.Interval)
	}
	if p.cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", p.cfg.Timeout)
	}
}

func TestCollect_Success(t *testing.T) {
	payload := fpmStatus{
		Pool:               "www",
		StartSince:         3600,
		AcceptedConn:       100,
		ListenQueue:        2,
		MaxListenQueue:     10,
		IdleProcesses:      5,
		ActiveProcesses:    2,
		TotalProcesses:     7,
		MaxChildrenReached: 1,
		SlowRequests:       3,
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	probe, err := NewPHPFPMProbe(map[string]interface{}{"endpoint": srv.URL + "/fpm-status"}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Build a map for easy lookup.
	byName := make(map[string]float64, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	checks := map[string]float64{
		"senhub.phpfpm.up":            1,
		"phpfpm.uptime":               3600,
		"phpfpm.accepted_connections": 100,
		"phpfpm.slow_requests":        3,
		"phpfpm.listen_queue.current": 2,
		"phpfpm.listen_queue.max":     10,
		"phpfpm.processes.active":     2,
		"phpfpm.processes.idle":       5,
		"phpfpm.processes.total":      7,
		"phpfpm.max_children_reached": 1,
	}

	for name, want := range checks {
		if got, ok := byName[name]; !ok {
			t.Errorf("metric %q missing from datapoints", name)
		} else if got != want {
			t.Errorf("metric %q = %v, want %v", name, got, want)
		}
	}

	// Verify pool tag is set.
	for _, dp := range points {
		if dp.Name == "senhub.phpfpm.up" {
			for _, tag := range dp.Tags {
				if tag.Key == "pool" && tag.Value != "www" {
					t.Errorf("pool tag = %q, want \"www\"", tag.Value)
				}
			}
		}
	}
}

func TestCollect_Unreachable(t *testing.T) {
	// Point at a port that refuses connections.
	probe, err := NewPHPFPMProbe(
		map[string]interface{}{"endpoint": "http://127.0.0.1:19999/fpm-status"},
		newTestLogger(t),
	)
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}

	points, _ := probe.Collect() // error is swallowed per probe contract

	found := false
	for _, dp := range points {
		if dp.Name == "senhub.phpfpm.up" {
			found = true
			if dp.Value != 0 {
				t.Errorf("senhub.phpfpm.up = %v on unreachable endpoint, want 0", dp.Value)
			}
		}
	}
	if !found {
		t.Error("senhub.phpfpm.up missing when endpoint is unreachable")
	}
}

func TestFetchStatus_JsonSuffix(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pool":"test","start since":0,"accepted conn":0,"listen queue":0,"max listen queue":0,"idle processes":0,"active processes":0,"total processes":0,"max children reached":0,"slow requests":0}`))
	}))
	defer srv.Close()

	probe, _ := NewPHPFPMProbe(map[string]interface{}{"endpoint": srv.URL + "/fpm-status"}, newTestLogger(t))
	p := probe.(*phpFPMProbe)
	_, err := p.fetchStatus()
	if err != nil {
		t.Fatalf("fetchStatus() error = %v", err)
	}
	if gotQuery != "json" {
		t.Errorf("query string = %q, want \"json\"", gotQuery)
	}
}

func TestFetchStatus_AlreadyHasJson(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pool":"test","start since":0,"accepted conn":0,"listen queue":0,"max listen queue":0,"idle processes":0,"active processes":0,"total processes":0,"max children reached":0,"slow requests":0}`))
	}))
	defer srv.Close()

	probe, _ := NewPHPFPMProbe(map[string]interface{}{"endpoint": srv.URL + "/fpm-status?json"}, newTestLogger(t))
	p := probe.(*phpFPMProbe)
	_, err := p.fetchStatus()
	if err != nil {
		t.Fatalf("fetchStatus() error = %v", err)
	}
	// Should not double-add json.
	if gotQuery != "json" {
		t.Errorf("query string = %q, want \"json\"", gotQuery)
	}
}

package http

import (
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestRemotelyChangedProbeShowsItsLiveState(t *testing.T) {
	router, dir := newOutputsTestRouter(t)

	probesDir := filepath.Join(dir, "probes.d")
	if err := os.MkdirAll(probesDir, 0o750); err != nil {
		t.Fatal(err)
	}
	probeFile := filepath.Join(probesDir, "10-cpu.yaml")
	if err := os.WriteFile(probeFile, []byte("- name: cpu\n  type: cpu\n  params:\n    interval: 10\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agentstate.SetActiveProbes([]string{"remote-id"})
	agentstate.SetActiveProbeNames(map[string]string{"cpu": "remote-id"})
	agentstate.SetActiveProbeIntervals(map[string]int{"cpu": 60})
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetActiveProbeNames(nil)
		agentstate.SetActiveProbeIntervals(nil)
	})

	code, resp := doJSON(t, router, "GET", "/api/"+testAdminKey+"/config/probes", nil)
	if code != 200 {
		t.Fatalf("GET /config/probes status = %d; want 200", code)
	}
	probes, ok := resp["probes"].([]interface{})
	if !ok {
		t.Fatalf("response has no probes list: %v", resp)
	}
	var cpu map[string]interface{}
	for _, p := range probes {
		if m, ok := p.(map[string]interface{}); ok && m["name"] == "cpu" {
			cpu = m
			break
		}
	}
	if cpu == nil {
		t.Fatalf("no probe named cpu in %v", probes)
	}
	if cpu["running"] != true {
		t.Errorf("cpu running = %v; want true", cpu["running"])
	}
	if cpu["interval"] != float64(60) {
		t.Errorf("cpu interval = %v; want 60", cpu["interval"])
	}
	if cpu["interval_remote"] != true {
		t.Errorf("cpu interval_remote = %v; want true", cpu["interval_remote"])
	}
	if cpu["file_interval"] != float64(10) {
		t.Errorf("cpu file_interval = %v; want 10", cpu["file_interval"])
	}
}

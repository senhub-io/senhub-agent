package opamp

import (
	"strings"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestBuildHealthComponentStatuses(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	start := now.Add(-time.Hour)

	in := healthInput{
		StartTime: start,
		Now:       now,
		Probes: []probeInput{
			{Name: "disabled", Enabled: false},
			{
				Name:    "running-ok",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running:   true,
					Health:    "ok",
					StartedAt: start.Add(-5 * time.Minute),
				},
			},
			{
				Name:    "running-failed",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running:   true,
					Health:    "failed",
					LastError: "boom",
					StartedAt: start,
				},
			},
			{
				Name:    "retrying",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running:   false,
					Health:    "failed",
					LastError: "boom",
				},
			},
			{
				Name:    "starting",
				Enabled: true,
				State:   agentstate.ProbeRunState{},
			},
		},
		Outputs: []outputInput{
			{
				Name: "bad",
				Failure: &agentstate.StrategyFailure{
					Reason: "create",
					Detail: "bad detail",
				},
			},
			{
				Name:      "degraded",
				Failing:   true,
				LastError: "last-error",
			},
			{Name: "good"},
		},
	}

	health := buildHealth(in, strings.ToUpper)

	if health.GetHealthy() {
		t.Fatal("root healthy = true, want false")
	}
	if health.GetStatus() != "degraded" {
		t.Fatalf("root status = %q, want degraded", health.GetStatus())
	}
	if health.GetStartTimeUnixNano() != uint64(start.UnixNano()) {
		t.Fatalf("root start time = %d, want %d", health.GetStartTimeUnixNano(), start.UnixNano())
	}
	if health.GetStatusTimeUnixNano() != uint64(now.UnixNano()) {
		t.Fatalf("root status time = %d, want %d", health.GetStatusTimeUnixNano(), now.UnixNano())
	}

	tests := []struct {
		key       string
		healthy   bool
		status    string
		lastError string
	}{
		{"probe:disabled", true, "disabled", ""},
		{"probe:running-ok", true, "running", ""},
		{"probe:running-failed", false, "failed", "BOOM"},
		{"probe:retrying", false, "retrying", "BOOM"},
		{"probe:starting", true, "starting", ""},
		{"output:bad", false, "failed", "CREATE: BAD DETAIL"},
		{"output:degraded", false, "degraded", "LAST-ERROR"},
		{"output:good", true, "running", ""},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			c, ok := health.GetComponentHealthMap()[tc.key]
			if !ok {
				t.Fatalf("component %q not found", tc.key)
			}
			if c.GetHealthy() != tc.healthy {
				t.Fatalf("healthy = %t, want %t", c.GetHealthy(), tc.healthy)
			}
			if c.GetStatus() != tc.status {
				t.Fatalf("status = %q, want %q", c.GetStatus(), tc.status)
			}
			if c.GetLastError() != tc.lastError {
				t.Fatalf("last error = %q, want %q", c.GetLastError(), tc.lastError)
			}
		})
	}
}

func TestBuildHealthRootRunning(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	start := now.Add(-time.Hour)

	health := buildHealth(healthInput{
		StartTime: start,
		Now:       now,
		Probes: []probeInput{
			{
				Name:    "running",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running: true,
					Health:  "ok",
				},
			},
		},
	}, nil)

	if !health.GetHealthy() {
		t.Fatal("root healthy = false, want true")
	}
	if health.GetStatus() != "running" {
		t.Fatalf("root status = %q, want running", health.GetStatus())
	}
}

func TestBuildHealthRootStarting(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	start := now.Add(-time.Hour)

	health := buildHealth(healthInput{
		StartTime: start,
		Now:       now,
		Probes: []probeInput{
			{Name: "starting", Enabled: true},
		},
	}, nil)

	if !health.GetHealthy() {
		t.Fatal("root healthy = false, want true")
	}
	if health.GetStatus() != "starting" {
		t.Fatalf("root status = %q, want starting", health.GetStatus())
	}
}

func TestBuildHealthDuplicateProbeNameKeepsFirst(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	health := buildHealth(healthInput{
		Now: now,
		Probes: []probeInput{
			{
				Name:    "dup",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running: true,
					Health:  "ok",
				},
			},
			{
				Name:    "dup",
				Enabled: true,
				State: agentstate.ProbeRunState{
					Running:   true,
					Health:    "failed",
					LastError: "later",
				},
			},
		},
	}, nil)

	if len(health.GetComponentHealthMap()) != 1 {
		t.Fatalf("component count = %d, want 1", len(health.GetComponentHealthMap()))
	}

	c, ok := health.GetComponentHealthMap()["probe:dup"]
	if !ok {
		t.Fatal("probe:dup not found")
	}
	if c.GetStatus() != "running" {
		t.Fatalf("probe:dup status = %q, want running (first occurrence)", c.GetStatus())
	}
	if !c.GetHealthy() {
		t.Fatal("probe:dup healthy = false, want true")
	}
}

func TestHealthFingerprintIgnoresTimeAndChangesWhenStatusChanges(t *testing.T) {
	now1 := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	now2 := now1.Add(time.Hour)
	start1 := now1.Add(-time.Hour)
	start2 := now2.Add(-time.Hour)

	build := func(now, start time.Time, failed bool) *protobufs.ComponentHealth {
		health := "ok"
		if failed {
			health = "failed"
		}
		return buildHealth(healthInput{
			StartTime: start,
			Now:       now,
			Probes: []probeInput{
				{
					Name:    "p",
					Enabled: true,
					State: agentstate.ProbeRunState{
						Running: true,
						Health:  health,
					},
				},
			},
		}, nil)
	}

	h1 := build(now1, start1, false)
	h2 := build(now2, start2, false)
	if healthFingerprint(h1) != healthFingerprint(h2) {
		t.Fatal("fingerprints differ when only time fields differ")
	}

	h3 := build(now1, start1, true)
	if healthFingerprint(h1) == healthFingerprint(h3) {
		t.Fatal("fingerprints equal when a status changed")
	}
}

func TestMakeRedact(t *testing.T) {
	const token = "alid_a_TOKEN"
	redact := makeRedact(token)

	tests := []struct {
		name string
		in   string
	}{
		{"token in text", "dial failed: alid_a_TOKEN"},
		{"authorization header", "Authorization: Bearer abc.def"},
		{"opamp endpoint", "wss://u:p@h/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := redact(tc.in)
			if strings.Contains(out, token) {
				t.Fatalf("redact(%q) = %q, still contains token", tc.in, out)
			}
		})
	}

	if out := redact("wss://u:p@h/x"); strings.Contains(out, ":p@") {
		t.Fatalf("redacted endpoint = %q, still carries the password", out)
	}

	out := redact("Authorization: Bearer abc.def")
	if !strings.Contains(out, "Bearer ***") {
		t.Fatalf("redacted authorization header = %q, want Bearer ***", out)
	}
	if strings.Contains(out, "abc.def") {
		t.Fatalf("redacted authorization header = %q, still contains token value", out)
	}
}

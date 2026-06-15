//go:build linux || windows || darwin

package process

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(nil, "probe.process.test")
}

// TestParseConfig verifies config parsing from the raw YAML params map.
func TestParseConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.interval != 30*time.Second {
			t.Errorf("interval = %v, want 30s", cfg.interval)
		}
		if !cfg.aggregate {
			t.Error("aggregate should default to true")
		}
		if cfg.byName != nil {
			t.Error("byName should default to nil (accept all)")
		}
		if cfg.topN != 0 {
			t.Errorf("topN = %d, want 0", cfg.topN)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{
			"interval": 60,
			"filter": map[string]interface{}{
				"by_name": "nginx|python",
				"by_user": "www-data",
				"top_n":   20,
			},
			"aggregate": map[string]interface{}{
				"enabled": false,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.interval != 60*time.Second {
			t.Errorf("interval = %v, want 60s", cfg.interval)
		}
		if cfg.byName == nil {
			t.Fatal("byName should be set")
		}
		if !cfg.byName.MatchString("nginx") {
			t.Error("byName should match 'nginx'")
		}
		if cfg.byName.MatchString("postgres") {
			t.Error("byName should not match 'postgres'")
		}
		if cfg.byUser != "www-data" {
			t.Errorf("byUser = %q, want www-data", cfg.byUser)
		}
		if cfg.topN != 20 {
			t.Errorf("topN = %d, want 20", cfg.topN)
		}
		if cfg.aggregate {
			t.Error("aggregate should be false")
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		_, err := parseConfig(map[string]interface{}{
			"filter": map[string]interface{}{
				"by_name": "[invalid",
			},
		})
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})
}

// TestCollect_CurrentHost verifies the probe can collect from the live host
// without panicking and emits at least one datapoint with expected metric names.
func TestCollect_CurrentHost(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"filter": map[string]interface{}{
			"top_n": 5,
		},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	points, _, err := collect(time.Now(), cfg, testLogger())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(points) == 0 {
		t.Fatal("collect returned zero datapoints — expected at least one running process")
	}

	names := map[string]bool{}
	for _, dp := range points {
		names[dp.Name] = true
	}

	required := []string{
		"process.cpu.utilization",
		"process.memory.usage",
		"process.memory.virtual_memory_usage",
		"process.threads",
	}
	for _, want := range required {
		if !names[want] {
			t.Errorf("metric %q not found in collected points", want)
		}
	}
}

// TestCollect_Aggregate verifies that aggregate=true emits process.count datapoints.
func TestCollect_Aggregate(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"aggregate": map[string]interface{}{
			"enabled": true,
		},
		"filter": map[string]interface{}{
			"top_n": 3,
		},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	points, _, err := collect(time.Now(), cfg, testLogger())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	hasCount := false
	for _, dp := range points {
		if dp.Name == "process.count" {
			hasCount = true
			break
		}
	}
	if !hasCount {
		t.Error("aggregate enabled but no process.count datapoint found")
	}
}

// TestCollect_AggregateDisabled verifies that aggregate=false suppresses process.count.
func TestCollect_AggregateDisabled(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"aggregate": map[string]interface{}{
			"enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	points, _, err := collect(time.Now(), cfg, testLogger())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "process.count" {
			t.Error("aggregate disabled but process.count datapoint was emitted")
		}
	}
}

//go:build linux || windows || darwin

package process

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"senhub-agent.go/internal/agent/services/data_store"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(ts []tags.Tag, key string) (string, bool) {
	for _, t := range ts {
		if t.Key == key {
			return t.Value, true
		}
	}
	return "", false
}

func TestCollectKeysAndCheckResultSuccess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	processName := filepath.Base(exe)

	cfg, err := parseConfig(map[string]interface{}{
		"filter": map[string]interface{}{
			"by_name": regexp.QuoteMeta(processName),
			"top_n":   5,
		},
		"aggregate": map[string]interface{}{
			"enabled": false,
		},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}

	const hostID = "test-host"
	baseTags, err := buildHostTags(hostID)
	if err != nil {
		t.Fatalf("buildHostTags: %v", err)
	}

	if v, ok := tagValue(baseTags, "host.id"); !ok || v != hostID {
		t.Fatalf("baseTags host.id = %q, ok=%v; want %q", v, ok, hostID)
	}

	points, snaps, err := collectWithBaseTags(time.Now(), cfg, testLogger(), baseTags)
	if err != nil {
		t.Fatalf("collectWithBaseTags: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("collectWithBaseTags returned zero datapoints")
	}

	haveEntityProcess := false
	for _, s := range snaps {
		if s.createTime > 0 {
			haveEntityProcess = true
			break
		}
	}
	if !haveEntityProcess {
		t.Fatal("no process snapshot with createTime; cannot verify process entity keys")
	}

	sawCheckResult := false
	for _, dp := range points {
		if v, ok := tagValue(dp.Tags, "host.id"); !ok {
			t.Errorf("datapoint %q missing host.id", dp.Name)
		} else if v != hostID {
			t.Errorf("datapoint %q host.id = %q, want %q", dp.Name, v, hostID)
		}

		if _, ok := tagValue(dp.Tags, "process.pid"); ok {
			if _, ok := tagValue(dp.Tags, "process.creation.time"); !ok {
				t.Errorf("per-process datapoint %q missing process.creation.time", dp.Name)
			}
		}

		if dp.Name == dbcommon.CheckResultMetric {
			sawCheckResult = true
			if dp.Value != 0 {
				t.Errorf("check result value = %v, want 0", dp.Value)
			}
			if metricTypeOf(dp) != "status" {
				t.Errorf("check result metric_type = %q, want status", metricTypeOf(dp))
			}
		}
	}

	if !sawCheckResult {
		t.Error("no check result datapoint found")
	}
}

func TestCheckResultPointFailure(t *testing.T) {
	baseTags := []tags.Tag{{Key: "host.id", Value: "fallback-host"}}
	dp := newCheckResultPoint(baseTags, time.Now(), errors.New("connection refused"))

	if dp.Name != dbcommon.CheckResultMetric {
		t.Errorf("check result name = %q, want %q", dp.Name, dbcommon.CheckResultMetric)
	}
	if dp.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result value = %v, want %v", dp.Value, float64(dbcommon.CheckTargetDown))
	}
	if metricTypeOf(dp) != "status" {
		t.Errorf("check result metric_type = %q, want status", metricTypeOf(dp))
	}
	if v, ok := tagValue(dp.Tags, "host.id"); !ok || v != "fallback-host" {
		t.Errorf("check result host.id = %q, ok=%v; want fallback-host", v, ok)
	}
}

func metricTypeOf(dp data_store.DataPoint) string {
	for _, tg := range dp.Tags {
		if tg.Key == "metric_type" {
			return tg.Value
		}
	}
	return ""
}

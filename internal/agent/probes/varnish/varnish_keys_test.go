package varnish

import (
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func tagValue(p data_store.DataPoint, key string) (string, bool) {
	for _, tag := range p.Tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func TestCollect_Success_IdentityKeysAndCheckResult(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "varnishstat")
	content := "#!/bin/sh\ncat <<'EOF'\n{\"MAIN.client_req\":{\"value\":10}}\nEOF\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	probe, err := NewVarnishProbe(map[string]interface{}{
		"varnishstat_path": script,
		"instance_name":    "myinstance",
	}, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe() error: %v", err)
	}
	vp := probe.(*VarnishProbe)
	wantID := vp.entitySrc.InstanceID()

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error %v; want nil", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	for _, p := range points {
		if got, ok := tagValue(p, "service.instance.id"); !ok || got != wantID {
			t.Errorf("%s: service.instance.id = %q, want %q", p.Name, got, wantID)
		}
	}

	var checkFound bool
	for _, p := range points {
		if p.Name != dbcommon.CheckResultMetric {
			continue
		}
		checkFound = true
		if p.Value != 0 {
			t.Errorf("%s = %v, want 0", dbcommon.CheckResultMetric, p.Value)
		}
		if got, ok := tagValue(p, "service.instance.id"); !ok || got != wantID {
			t.Errorf("%s: service.instance.id = %q, want %q", dbcommon.CheckResultMetric, got, wantID)
		}
		if got, ok := tagValue(p, "metric_type"); !ok || got != "status" {
			t.Errorf("%s: metric_type = %q, want status", dbcommon.CheckResultMetric, got)
		}
	}
	if !checkFound {
		t.Errorf("metric %q not emitted", dbcommon.CheckResultMetric)
	}
}

func TestCollect_FailedConnection_IdentityKeyAndCheckResult(t *testing.T) {
	probe, err := NewVarnishProbe(map[string]interface{}{
		"varnishstat_path": "/nonexistent/varnishstat_does_not_exist",
	}, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe() error: %v", err)
	}
	vp := probe.(*VarnishProbe)
	wantID := vp.entitySrc.InstanceID()

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error %v; want nil (down probe is not a collection error)", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	_, runErr := vp.runVarnishstat()
	wantCheck := float64(dbcommon.ClassifyError(runErr))

	var upFound, checkFound bool
	for _, p := range points {
		if p.Name == "senhub.varnish.up" {
			upFound = true
			if p.Value != 0 {
				t.Errorf("senhub.varnish.up = %v, want 0", p.Value)
			}
			if got, ok := tagValue(p, "service.instance.id"); !ok || got != wantID {
				t.Errorf("senhub.varnish.up: service.instance.id = %q, want %q", got, wantID)
			}
			if got, ok := tagValue(p, "metric_type"); !ok || got != "status" {
				t.Errorf("senhub.varnish.up: metric_type = %q, want status", got)
			}
		}
		if p.Name == dbcommon.CheckResultMetric {
			checkFound = true
			if p.Value != wantCheck {
				t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, p.Value, wantCheck)
			}
			if got, ok := tagValue(p, "service.instance.id"); !ok || got != wantID {
				t.Errorf("%s: service.instance.id = %q, want %q", dbcommon.CheckResultMetric, got, wantID)
			}
			if got, ok := tagValue(p, "metric_type"); !ok || got != "status" {
				t.Errorf("%s: metric_type = %q, want status", dbcommon.CheckResultMetric, got)
			}
		}
	}
	if !upFound {
		t.Error("Collect() did not emit senhub.varnish.up=0 when varnishstat is absent")
	}
	if !checkFound {
		t.Errorf("metric %q not emitted", dbcommon.CheckResultMetric)
	}
}

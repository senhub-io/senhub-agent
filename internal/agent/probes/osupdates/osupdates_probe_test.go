package osupdates

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

type stubCollector struct {
	status updatesStatus
	err    error
}

func (s stubCollector) collect(context.Context) (updatesStatus, error) {
	return s.status, s.err
}

func TestNewOSUpdatesProbe_Defaults(t *testing.T) {
	probe, err := NewOSUpdatesProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewOSUpdatesProbe: %v", err)
	}
	if got, want := probe.GetInterval(), defaultInterval; got != want {
		t.Errorf("interval: got %v, want %v", got, want)
	}
	p := probe.(*OSUpdatesProbe)
	if got, want := p.cfg.CommandTimeout, defaultCommandTimeout; got != want {
		t.Errorf("command timeout: got %v, want %v", got, want)
	}
	if p.collector == nil {
		t.Error("collector must be set by the constructor")
	}
	if probe.EntitySource() == nil {
		t.Error("EntitySource() must not be nil (BaseProbe NoOp fallback expected)")
	}
}

func TestNewOSUpdatesProbe_ConfigParse(t *testing.T) {
	probe, err := NewOSUpdatesProbe(map[string]interface{}{
		"interval":        1800,
		"command_timeout": 30,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewOSUpdatesProbe: %v", err)
	}
	if got, want := probe.GetInterval(), 1800*time.Second; got != want {
		t.Errorf("interval: got %v, want %v", got, want)
	}
	if got, want := probe.(*OSUpdatesProbe).cfg.CommandTimeout, 30*time.Second; got != want {
		t.Errorf("command timeout: got %v, want %v", got, want)
	}
}

func pointsByName(points []data_store.DataPoint) map[string]data_store.DataPoint {
	byName := make(map[string]data_store.DataPoint, len(points))
	for _, p := range points {
		byName[p.Name] = p
	}
	return byName
}

func tagValue(dp data_store.DataPoint, key string) (string, bool) {
	for _, tag := range dp.Tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func TestCollect_SuccessEmitsAllMetrics(t *testing.T) {
	probe, err := NewOSUpdatesProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewOSUpdatesProbe: %v", err)
	}
	p := probe.(*OSUpdatesProbe)
	p.collector = stubCollector{status: updatesStatus{
		pending:         5,
		pendingSecurity: 2,
		rebootRequired:  true,
		packageManager:  "apt",
	}}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("got %d points, want 4", len(points))
	}

	byName := pointsByName(points)
	expect := map[string]float64{
		"senhub.os.updates.up":               1,
		"senhub.os.updates.pending":          5,
		"senhub.os.updates.pending.security": 2,
		"senhub.os.updates.reboot_required":  1,
	}
	for name, want := range expect {
		dp, ok := byName[name]
		if !ok {
			t.Errorf("missing metric %s", name)
			continue
		}
		if dp.Value != want {
			t.Errorf("%s: got %v, want %v", name, dp.Value, want)
		}
		if pm, ok := tagValue(dp, "package_manager"); !ok || pm != "apt" {
			t.Errorf("%s: package_manager tag = %q (present=%v), want \"apt\"", name, pm, ok)
		}
	}
}

func TestCollect_BackendFailureEmitsUpZeroOnly(t *testing.T) {
	probe, err := NewOSUpdatesProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewOSUpdatesProbe: %v", err)
	}
	p := probe.(*OSUpdatesProbe)
	p.collector = stubCollector{err: errors.New("backend unreachable")}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not fail on backend errors (graceful degradation): %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("got %d points, want exactly 1 (the up=0 signal)", len(points))
	}
	dp := points[0]
	if dp.Name != "senhub.os.updates.up" || dp.Value != 0 {
		t.Errorf("got %s=%v, want senhub.os.updates.up=0", dp.Name, dp.Value)
	}
}

func TestCollect_EnrichesProbeTags(t *testing.T) {
	probe, err := NewOSUpdatesProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewOSUpdatesProbe: %v", err)
	}
	p := probe.(*OSUpdatesProbe)
	p.SetName("os-updates")
	p.collector = stubCollector{status: updatesStatus{packageManager: "dnf"}}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		if pt, ok := tagValue(dp, "probe_type"); !ok || pt != ProbeType {
			t.Errorf("%s: probe_type tag = %q (present=%v), want %q", dp.Name, pt, ok, ProbeType)
		}
		if mt, ok := tagValue(dp, "metric_type"); !ok || mt != "os_updates" {
			t.Errorf("%s: metric_type tag = %q (present=%v), want \"os_updates\"", dp.Name, mt, ok)
		}
	}
}

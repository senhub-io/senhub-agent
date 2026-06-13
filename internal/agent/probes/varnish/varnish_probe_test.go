package varnish

import (
	"testing"
	"time"
)

func TestNewVarnishProbe_Defaults(t *testing.T) {
	probe, err := NewVarnishProbe(map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe() error: %v", err)
	}
	vp := probe.(*VarnishProbe)
	if vp.cfg.VarnishstatPath != defaultVarnishstatPath {
		t.Errorf("VarnishstatPath = %q, want %q", vp.cfg.VarnishstatPath, defaultVarnishstatPath)
	}
	if vp.cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", vp.cfg.Interval, defaultInterval)
	}
	if vp.cfg.InstanceName != "" {
		t.Errorf("InstanceName = %q, want empty", vp.cfg.InstanceName)
	}
}

func TestNewVarnishProbe_CustomConfig(t *testing.T) {
	cfg := map[string]interface{}{
		"varnishstat_path": "/usr/bin/varnishstat",
		"instance_name":    "myinstance",
		"interval":         30,
	}
	probe, err := NewVarnishProbe(cfg, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe() error: %v", err)
	}
	vp := probe.(*VarnishProbe)
	if vp.cfg.VarnishstatPath != "/usr/bin/varnishstat" {
		t.Errorf("VarnishstatPath = %q, want /usr/bin/varnishstat", vp.cfg.VarnishstatPath)
	}
	if vp.cfg.InstanceName != "myinstance" {
		t.Errorf("InstanceName = %q, want myinstance", vp.cfg.InstanceName)
	}
	if vp.cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", vp.cfg.Interval)
	}
}

func TestVarnishProbe_ProbeType(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{}, nil)
	vp := probe.(*VarnishProbe)
	if vp.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", vp.GetProbeType(), ProbeType)
	}
}

func TestVarnishProbe_ShouldStart(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{}, nil)
	if !probe.ShouldStart() {
		t.Error("ShouldStart() should return true")
	}
}

func TestVarnishProbe_GetInterval(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{"interval": 120}, nil)
	if probe.GetInterval() != 120*time.Second {
		t.Errorf("GetInterval() = %v, want 120s", probe.GetInterval())
	}
}

// TestBuildDataPoints_Up exercises the data-point builder with a synthetic
// varnishstat output. It verifies:
//   - senhub.varnish.up is 1 on success
//   - collapsed metrics (varnish.cache.operations, varnish.thread.operations)
//     carry the discriminant tag
//   - memory accumulates SMA/SMF g_bytes entries
func TestBuildDataPoints_Up(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{}, nil)
	vp := probe.(*VarnishProbe)

	stats := map[string]varnishStat{
		"MAIN.cache_hit":       {Value: 100},
		"MAIN.cache_miss":      {Value: 20},
		"MAIN.cache_hitpass":   {Value: 5},
		"MAIN.client_req":      {Value: 125},
		"MAIN.backend_conn":    {Value: 50},
		"MAIN.backend_fail":    {Value: 2},
		"MAIN.backend_reuse":   {Value: 48},
		"MAIN.threads_created": {Value: 10},
		"MAIN.threads_destroyed": {Value: 3},
		"MAIN.threads_failed":  {Value: 0},
		"MAIN.sess_conn":       {Value: 200},
		"MAIN.sess_drop":       {Value: 1},
		"MAIN.n_object":        {Value: 42},
		"SMA.s0.g_bytes":       {Value: 1048576},
		"SMA.transient.g_bytes": {Value: 524288},
	}

	points := vp.buildDataPoints(stats, time.Now())

	byName := make(map[string][]float32)
	for _, p := range points {
		byName[p.Name] = append(byName[p.Name], p.Value)
	}

	tests := []struct {
		metric    string
		wantCount int
	}{
		{"senhub.varnish.up", 1},
		{"varnish.cache.operations", 3},        // hit / miss / hitpass
		{"varnish.client.requests.received", 1},
		{"varnish.backend.connections.success", 1},
		{"varnish.backend.connections.fail", 1},
		{"varnish.backend.connections.reused", 1},
		{"varnish.thread.operations", 3},        // created / destroyed / failed
		{"varnish.session.connections", 1},
		{"varnish.session.dropped", 1},
		{"varnish.objects.stored", 1},
		{"varnish.memory.allocated", 1},
	}

	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			vals, ok := byName[tt.metric]
			if !ok {
				t.Fatalf("metric %q not emitted", tt.metric)
			}
			if len(vals) != tt.wantCount {
				t.Errorf("metric %q: got %d datapoints, want %d", tt.metric, len(vals), tt.wantCount)
			}
		})
	}

	// Verify memory sum
	memVals := byName["varnish.memory.allocated"]
	if len(memVals) != 1 || memVals[0] != float32(1048576+524288) {
		t.Errorf("varnish.memory.allocated = %v, want %v", memVals, float32(1048576+524288))
	}

	// Verify up=1
	upVals := byName["senhub.varnish.up"]
	if len(upVals) != 1 || upVals[0] != 1 {
		t.Errorf("senhub.varnish.up = %v, want [1]", upVals)
	}
}

// TestCollect_VarnishstatNotFound verifies that when varnishstat is missing
// the probe still emits senhub.varnish.up=0 without returning an error.
func TestCollect_VarnishstatNotFound(t *testing.T) {
	probe, err := NewVarnishProbe(map[string]interface{}{
		"varnishstat_path": "/nonexistent/varnishstat_does_not_exist",
	}, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe() error: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error %v; want nil (down probe is not a collection error)", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints; expected at least senhub.varnish.up=0")
	}

	var found bool
	for _, p := range points {
		if p.Name == "senhub.varnish.up" && p.Value == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Collect() did not emit senhub.varnish.up=0 when varnishstat is absent")
	}
}

// TestCacheOperations_Tags ensures discriminant tags are set on collapsed metrics.
func TestCacheOperations_Tags(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{}, nil)
	vp := probe.(*VarnishProbe)

	stats := map[string]varnishStat{
		"MAIN.cache_hit":     {Value: 10},
		"MAIN.cache_miss":    {Value: 5},
		"MAIN.cache_hitpass": {Value: 1},
	}
	points := vp.buildDataPoints(stats, time.Now())

	resultValues := make(map[string]bool)
	for _, p := range points {
		if p.Name != "varnish.cache.operations" {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == "result" {
				resultValues[tag.Value] = true
			}
		}
	}

	for _, expected := range []string{"hit", "miss", "hitpass"} {
		if !resultValues[expected] {
			t.Errorf("varnish.cache.operations missing result=%q tag", expected)
		}
	}
}

// TestThreadOperations_Tags ensures operation discriminant tags are emitted.
func TestThreadOperations_Tags(t *testing.T) {
	probe, _ := NewVarnishProbe(map[string]interface{}{}, nil)
	vp := probe.(*VarnishProbe)

	stats := map[string]varnishStat{
		"MAIN.threads_created":   {Value: 5},
		"MAIN.threads_destroyed": {Value: 2},
		"MAIN.threads_failed":    {Value: 0},
	}
	points := vp.buildDataPoints(stats, time.Now())

	opValues := make(map[string]bool)
	for _, p := range points {
		if p.Name != "varnish.thread.operations" {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == "operation" {
				opValues[tag.Value] = true
			}
		}
	}

	for _, expected := range []string{"created", "destroyed", "failed"} {
		if !opValues[expected] {
			t.Errorf("varnish.thread.operations missing operation=%q tag", expected)
		}
	}
}

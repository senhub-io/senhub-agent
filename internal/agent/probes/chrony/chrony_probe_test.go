package chrony

import (
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// TestParseTracking_Normal verifies a well-formed chronyc -c tracking line.
func TestParseTracking_Normal(t *testing.T) {
	// Fields: ref_id, stratum, ref_time, system_time, last_offset,
	// rms_offset, freq_ppm, residual_freq, skew, root_delay,
	// root_dispersion, update_interval, leap_status
	line := "C0A80101,2,1686825600.000,0.000012345,0.000000123,0.000000456,1.234,-0.001,0.012,0.001234,0.002345,64.0,Normal"

	res := parseTracking(line)
	if res.err != nil {
		t.Fatalf("parseTracking error: %v", res.err)
	}
	if got, want := res.stratum, float32(2); got != want {
		t.Errorf("stratum: got %v, want %v", got, want)
	}
	if got := res.systemTimeS; got < 0.000012 || got > 0.000013 {
		t.Errorf("systemTimeS: got %v, out of expected range", got)
	}
	if got := res.freqPPM; got < 1.23 || got > 1.24 {
		t.Errorf("freqPPM: got %v, out of range", got)
	}
	if got := res.skewPPM; got < 0.011 || got > 0.013 {
		t.Errorf("skewPPM: got %v, out of range", got)
	}
	if got := res.rootDelayS; got < 0.001 || got > 0.002 {
		t.Errorf("rootDelayS: got %v, out of range", got)
	}
	if got := res.rootDispersionS; got < 0.002 || got > 0.003 {
		t.Errorf("rootDispersionS: got %v, out of range", got)
	}
	if got, want := res.leapStatus, leapNormal; got != want {
		t.Errorf("leapStatus: got %q, want %q", got, want)
	}
}

// TestParseTracking_TooFewFields verifies the error path for short output.
func TestParseTracking_TooFewFields(t *testing.T) {
	res := parseTracking("only,three,fields")
	if res.err == nil {
		t.Fatal("expected error for too-few fields, got nil")
	}
}

// TestParseTracking_BadFloat verifies the error path for non-numeric fields.
func TestParseTracking_BadFloat(t *testing.T) {
	// Replace stratum (index 1) with a non-number.
	line := "C0A80101,BADNUM,1686825600.000,0.000012345,0.000000123,0.000000456,1.234,-0.001,0.012,0.001234,0.002345,64.0,Normal"
	res := parseTracking(line)
	if res.err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestLeapToFloat covers every branch of the leap-status conversion.
func TestLeapToFloat(t *testing.T) {
	cases := []struct {
		status string
		want   float32
	}{
		{leapNormal, 0},
		{leapInsert, 1},
		{leapDelete, 2},
		{leapNotSynced, 3},
		{"anything else", 3},
	}
	for _, tc := range cases {
		if got := leapToFloat(tc.status); got != tc.want {
			t.Errorf("leapToFloat(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestParseTracking_LeapStatuses verifies each leap-status string parses.
func TestParseTracking_LeapStatuses(t *testing.T) {
	base := "C0A80101,2,1686825600.000,0.000012,0.000001,0.000002,1.234,-0.001,0.012,0.001,0.002,64.0,"
	cases := []struct {
		status string
		want   float32
	}{
		{leapNormal, 0},
		{leapInsert, 1},
		{leapDelete, 2},
		{leapNotSynced, 3},
	}
	for _, tc := range cases {
		res := parseTracking(base + tc.status)
		if res.err != nil {
			t.Errorf("parseTracking(%q): %v", tc.status, res.err)
			continue
		}
		if got := leapToFloat(res.leapStatus); got != tc.want {
			t.Errorf("leap %q: got %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestCollect_Success asserts that a successful run emits 8 datapoints
// (senhub.chrony.up + 7 NTP metrics) and that senhub.chrony.up = 1.
func TestCollect_Success(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	p.SetName("chrony-test")
	p.run = func() trackingResult {
		return trackingResult{
			stratum:         2,
			systemTimeS:     0.000123,
			freqPPM:         -3.5,
			skewPPM:         0.05,
			rootDelayS:      0.0015,
			rootDispersionS: 0.0025,
			leapStatus:      leapNormal,
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	if got, want := len(points), 8; got != want {
		t.Errorf("Collect: got %d points, want %d", got, want)
	}

	up := findByName(points, "senhub.chrony.up")
	if up == nil {
		t.Fatal("senhub.chrony.up not found")
	}
	if up.Value != 1 {
		t.Errorf("senhub.chrony.up = %v, want 1", up.Value)
	}

	// ntp.time.offset = 0.000123 * 1000 = 0.123 ms
	offset := findByName(points, "ntp.time.offset")
	if offset == nil {
		t.Fatal("ntp.time.offset not found")
	}
	if got := float64(offset.Value); got < 0.12 || got > 0.13 {
		t.Errorf("ntp.time.offset: got %v ms, want ~0.123 ms", got)
	}

	// Every datapoint must carry metric_type=time_sync.
	for _, pt := range points {
		if !hasTag(pt.Tags, "metric_type", "time_sync") {
			t.Errorf("point %s missing metric_type=time_sync", pt.Name)
		}
	}
}

// TestCollect_Failure asserts that a subprocess failure emits only
// senhub.chrony.up=0 and no other metrics.
func TestCollect_Failure(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	p.SetName("chrony-test")
	p.run = func() trackingResult {
		return trackingResult{err: fmt.Errorf("chronyc: executable file not found")}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	up := findByName(points, "senhub.chrony.up")
	if up == nil {
		t.Fatal("senhub.chrony.up not found")
	}
	if up.Value != 0 {
		t.Errorf("senhub.chrony.up = %v, want 0", up.Value)
	}
	for _, pt := range points {
		if pt.Name != "senhub.chrony.up" {
			t.Errorf("unexpected point %s on failure", pt.Name)
		}
	}
}

// TestNewChronyProbe_Defaults verifies default config values.
func TestNewChronyProbe_Defaults(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	if p.cfg.ChronyPath != defaultChronyc {
		t.Errorf("ChronyPath: got %q, want %q", p.cfg.ChronyPath, defaultChronyc)
	}
	if p.cfg.Interval != defaultInterval {
		t.Errorf("Interval: got %v, want %v", p.cfg.Interval, defaultInterval)
	}
}

// TestNewChronyProbe_CustomConfig verifies config overrides.
func TestNewChronyProbe_CustomConfig(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{
		"chronyc_path": "/usr/local/bin/chronyc",
		"interval":     60,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	if p.cfg.ChronyPath != "/usr/local/bin/chronyc" {
		t.Errorf("ChronyPath: got %q", p.cfg.ChronyPath)
	}
	if p.cfg.Interval != 60*time.Second {
		t.Errorf("Interval: got %v, want 60s", p.cfg.Interval)
	}
}

// TestProbeType checks the stable probe type identifier.
func TestProbeType(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	if got := p.GetProbeType(); got != ProbeType {
		t.Errorf("GetProbeType: got %q, want %q", got, ProbeType)
	}
}

// TestShouldStart verifies the probe always starts (no static filter).
func TestShouldStart(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	if !raw.ShouldStart() {
		t.Error("ShouldStart: got false, want true")
	}
}

// TestGetInterval verifies that a non-default interval is returned.
func TestGetInterval(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{"interval": 120}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	if got, want := raw.GetInterval(), 120*time.Second; got != want {
		t.Errorf("GetInterval: got %v, want %v", got, want)
	}
}

// findByName returns the first DataPoint with a matching name, or nil.
func findByName(pts []data_store.DataPoint, name string) *data_store.DataPoint {
	for i := range pts {
		if pts[i].Name == name {
			return &pts[i]
		}
	}
	return nil
}

// hasTag checks whether a tag slice contains a key=value pair.
func hasTag(ts []tags.Tag, key, value string) bool {
	for _, t := range ts {
		if t.Key == key && t.Value == value {
			return true
		}
	}
	return false
}

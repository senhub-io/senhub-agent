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
// The fixture is a VERBATIM capture from chrony 4.5, not a hand-written line.
//
// The previous fixture invented a 13-field format with no reference-address
// column, so the parser was verified against an imitation of chronyc rather
// than against chronyc. It passed for two years while the probe failed on
// every synchronised host in the field.
const realTrackingLine = "65D8456C,2620:2d:4000:1::3123,3,1786708478.062514584,0.000106370,-0.000040664,0.000311689,0.647,-0.001,0.099,0.015753603,0.000998904,1026.5,Normal"

func TestParseTracking_RealChronycOutput(t *testing.T) {
	res := parseTracking(realTrackingLine)
	if res.err != nil {
		t.Fatalf("parseTracking on real output: %v", res.err)
	}
	for _, c := range []struct {
		name string
		got  float64
		want float64
	}{
		{"stratum", res.stratum, 3},
		{"systemTimeS", res.systemTimeS, 0.000106370},
		{"freqPPM", res.freqPPM, 0.647},
		{"skewPPM", res.skewPPM, 0.099},
		{"rootDelayS", res.rootDelayS, 0.015753603},
		{"rootDispersionS", res.rootDispersionS, 0.000998904},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if res.leapStatus != "Normal" {
		t.Errorf("leapStatus = %q, want Normal", res.leapStatus)
	}
}

// The reported failure, as a regression test: an IPv4 reference address read as
// the stratum. The defect only appeared once a host was SYNCHRONISED, because
// an unsynchronised chrony leaves the address empty — so the probe worked
// exactly as long as there was nothing to measure.
func TestParseTracking_SynchronisedHostWithIPv4Reference(t *testing.T) {
	line := "6DBEB1CD,109.190.177.205,2,1786708478.062,0.000106370,-0.000040664,0.000311689,0.647,-0.001,0.099,0.015753603,0.000998904,1026.5,Normal"
	res := parseTracking(line)
	if res.err != nil {
		t.Fatalf("the reported failure is back: %v", res.err)
	}
	if res.stratum != 2 {
		t.Errorf("stratum = %v, want 2 — the address column was read again", res.stratum)
	}
}

// Same for an IPv6 reference, which the reporter also hit.
func TestParseTracking_SynchronisedHostWithIPv6Reference(t *testing.T) {
	line := "F46D8B3E,2a01cb00129ea8030000000000000137.ipv6.abo.wanadoo.fr,4,1786708478.062,0.000106370,-0.000040664,0.000311689,0.647,-0.001,0.099,0.015753603,0.000998904,1026.5,Normal"
	res := parseTracking(line)
	if res.err != nil {
		t.Fatalf("IPv6 reference still breaks parsing: %v", res.err)
	}
	if res.stratum != 4 {
		t.Errorf("stratum = %v, want 4", res.stratum)
	}
}

// An unsynchronised host leaves the address empty. It must parse, because that
// is a normal state — a machine that has not acquired a source yet.
func TestParseTracking_UnsynchronisedHost(t *testing.T) {
	line := "00000000,,0,0.000000000,0.000000000,0.000000000,0.000000000,0.000,0.000,0.000,0.000000000,0.000000000,0.0,Not synchronised"
	res := parseTracking(line)
	if res.err != nil {
		t.Fatalf("an unsynchronised host must still parse: %v", res.err)
	}
	if res.stratum != 0 {
		t.Errorf("stratum = %v, want 0", res.stratum)
	}
	if res.leapStatus != "Not synchronised" {
		t.Errorf("leapStatus = %q", res.leapStatus)
	}
}

// A thirteen-field line is what the old fixture looked like. It must now be
// rejected rather than silently parsed with every column shifted.
func TestParseTracking_RejectsTheOldInventedFormat(t *testing.T) {
	line := "C0A80101,2,1686825600.000,0.000012345,0.000000123,0.000000456,1.234,-0.001,0.012,0.001234,0.002345,64.0,Normal"
	if res := parseTracking(line); res.err == nil {
		t.Fatal("a 13-field line was accepted; the field count no longer guards the shift")
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
		want   float64
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
	base := "C0A80101,109.190.177.205,2,1686825600.000,0.000012,0.000001,0.000002,1.234,-0.001,0.012,0.001,0.002,64.0,"
	cases := []struct {
		status string
		want   float64
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

// TestCollect_Success asserts that a successful run emits up, the seven NTP
// metrics, and the four one-hot state series that say WHY up has its value.
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
	if got, want := len(points), 12; got != want {
		t.Errorf("Collect: got %d points, want %d", got, want)
	}
	// The reason is readable without opening the log: exactly one state series
	// is 1, and on success it is "ok".
	assertOnlyReason(t, points, reasonOK)

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
	// On failure only up and the state series are emitted — never a measurement,
	// since there is nothing to measure. The state series are what make the
	// failure legible without the log.
	for _, pt := range points {
		if pt.Name != "senhub.chrony.up" && pt.Name != "senhub.chrony.state" {
			t.Errorf("a measurement was emitted on failure: %s", pt.Name)
		}
	}
	assertOnlyReason(t, points, reasonNotInstalled)
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

// assertOnlyReason checks that exactly one state series is set, and which.
func assertOnlyReason(t *testing.T, points []data_store.DataPoint, want string) {
	t.Helper()
	var on []string
	for _, pt := range points {
		if pt.Name != "senhub.chrony.state" || pt.Value != 1 {
			continue
		}
		for _, tag := range pt.Tags {
			if tag.Key == "reason" {
				on = append(on, tag.Value)
			}
		}
	}
	if len(on) != 1 {
		t.Fatalf("state series set: %v, want exactly one", on)
	}
	if on[0] != want {
		t.Errorf("reason = %q, want %q", on[0], want)
	}
}

// The reported ambiguity: chronyc absent and chronyc unreadable both produced
// up=0 and nothing else, so an operator could not tell a host without NTP from
// a probe that cannot read the one it has. Only one of those needs action.
func TestCollect_DistinguishesMissingChronycFromABadRead(t *testing.T) {
	newProbe := func(run runFunc) *ChronyProbe {
		raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
		if err != nil {
			t.Fatalf("NewChronyProbe: %v", err)
		}
		p := raw.(*ChronyProbe)
		p.SetName("chrony-test")
		p.run = run
		return p
	}

	absent := newProbe(func() trackingResult {
		return trackingResult{err: fmt.Errorf("chronyc: exec: \"chronyc\": executable file not found in $PATH")}
	})
	pts, _ := absent.Collect()
	assertOnlyReason(t, pts, reasonNotInstalled)

	unreadable := newProbe(func() trackingResult {
		return parseTracking("garbage,without,enough,fields")
	})
	pts, _ = unreadable.Collect()
	assertOnlyReason(t, pts, reasonParseFailed)
}

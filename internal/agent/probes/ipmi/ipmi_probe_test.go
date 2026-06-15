package ipmi

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

var errIpmitoolNotFound = errors.New("exec: ipmitool: not found")

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestModuleLogger(t *testing.T) *logger.ModuleLogger {
	t.Helper()
	return logger.NewModuleLogger(testBaseLogger(), "probe.ipmi.test")
}

func newBaseProbe() *types.BaseProbe {
	return &types.BaseProbe{}
}

// Synthetic ipmitool output representing a healthy server.
const sampleSdrOutput = `CPU Temp        | 45 degrees C      | ok
System Temp     | 30 degrees C      | ok
PCH Temp        | 52 degrees C      | ok
FAN1            | 3000 RPM          | ok
FAN2            | 3100 RPM          | ok
FAN_MOD1        | 2800 RPM          | nc
12V             | 12.06 Volts       | ok
5V              | 5.11 Volts        | ok
3.3V            | 3.34 Volts        | ok
PS1 Status      | Presence Detected | ok
PS2 Status      | no reading        | ns
Bad Sensor      | nodata            | ok
`

func TestParseSdrOutput_CountRows(t *testing.T) {
	rows := parseSdrOutput(sampleSdrOutput)
	if len(rows) != 12 {
		t.Errorf("expected 12 rows, got %d", len(rows))
	}
}

func TestParseSdrOutput_FieldTrimming(t *testing.T) {
	rows := parseSdrOutput(sampleSdrOutput)
	if rows[0].name != "CPU Temp" {
		t.Errorf("name not trimmed: %q", rows[0].name)
	}
	if rows[0].value != "45 degrees C" {
		t.Errorf("value not trimmed: %q", rows[0].value)
	}
	if rows[0].status != "ok" {
		t.Errorf("status not trimmed: %q", rows[0].status)
	}
}

func TestParseSdrOutput_EmptyLines(t *testing.T) {
	rows := parseSdrOutput("\n\n")
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for blank input, got %d", len(rows))
	}
}

func TestParseSdrOutput_TwoFields(t *testing.T) {
	// Lines with fewer than 3 pipe-separated fields are skipped.
	rows := parseSdrOutput("CPU Temp | 45 degrees C\n")
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for line with <3 fields, got %d", len(rows))
	}
}

func TestParseValueUnit_Temperature(t *testing.T) {
	v, unit, typ := parseValueUnit("45 degrees C")
	if v == nil || *v != 45 {
		t.Errorf("expected 45, got %v", v)
	}
	if unit != "degrees C" {
		t.Errorf("expected 'degrees C', got %q", unit)
	}
	if typ != "temperature" {
		t.Errorf("expected 'temperature', got %q", typ)
	}
}

func TestParseValueUnit_Fan(t *testing.T) {
	v, _, typ := parseValueUnit("3000 RPM")
	if v == nil || *v != 3000 {
		t.Errorf("expected 3000, got %v", v)
	}
	if typ != "fan" {
		t.Errorf("expected 'fan', got %q", typ)
	}
}

func TestParseValueUnit_Voltage(t *testing.T) {
	v, _, typ := parseValueUnit("12.06 Volts")
	if v == nil || *v != 12.06 {
		t.Errorf("expected 12.06, got %v", v)
	}
	if typ != "voltage" {
		t.Errorf("expected 'voltage', got %q", typ)
	}
}

func TestParseValueUnit_NoReading(t *testing.T) {
	v, unit, typ := parseValueUnit("no reading")
	if v != nil {
		t.Errorf("expected nil for 'no reading', got %v", v)
	}
	if unit != "" || typ != "" {
		t.Errorf("expected empty unit/type, got %q/%q", unit, typ)
	}
}

func TestParseValueUnit_NonNumeric(t *testing.T) {
	v, unit, _ := parseValueUnit("Presence Detected")
	if v != nil {
		t.Errorf("expected nil for non-numeric, got %v", v)
	}
	if unit != "Detected" {
		t.Errorf("expected 'Detected', got %q", unit)
	}
}

func TestParseValueUnit_NoSpace(t *testing.T) {
	v, _, _ := parseValueUnit("nodata")
	if v != nil {
		t.Errorf("expected nil for value without space, got %v", v)
	}
}

func TestIsStatusOk(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"ok", true},
		{"OK", true},
		{"nc", true},
		{"cr", false},
		{"nr", false},
		{"ns", false},
		{"na", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := isStatusOk(tt.status); got != tt.want {
				t.Errorf("isStatusOk(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// TestCollect_HappyPath verifies that a successful ipmitool run
// emits hardware metrics and senhub.ipmi.up=1.
func TestCollect_HappyPath(t *testing.T) {
	output := "CPU Temp        | 45 degrees C      | ok\n" +
		"FAN1            | 3000 RPM          | ok\n"

	cfg := ipmiConfig{
		Mode:         "local",
		IpmitoolPath: defaultIpmitoolPath,
		Interval:     defaultInterval,
	}
	p := &ipmiProbe{
		BaseProbe:    newBaseProbe(),
		cfg:          cfg,
		moduleLogger: newTestModuleLogger(t),
		runner:       func(_ ipmiConfig) (string, error) { return output, nil },
	}
	p.SetProbeType(ProbeType)
	p.SetName("test-ipmi")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// at minimum: sensor.status x2 + temperature + fan.speed + ipmi.up
	if len(points) < 5 {
		t.Errorf("expected >=5 datapoints, got %d", len(points))
	}

	names := make(map[string]bool)
	for _, dp := range points {
		names[dp.Name] = true
	}
	for _, want := range []string{
		"hardware.sensor.status",
		"hardware.temperature",
		"hardware.fan.speed",
		"senhub.ipmi.up",
	} {
		if !names[want] {
			t.Errorf("missing datapoint %q; have: %v", want, names)
		}
	}

	for _, dp := range points {
		if dp.Name == "senhub.ipmi.up" && dp.Value != 1 {
			t.Errorf("senhub.ipmi.up = %v, want 1", dp.Value)
		}
	}
}

// TestCollect_IpmitoolFailure verifies that runner errors result in
// senhub.ipmi.up=0 without a collection error.
func TestCollect_IpmitoolFailure(t *testing.T) {
	cfg := ipmiConfig{
		Mode:         "local",
		IpmitoolPath: defaultIpmitoolPath,
		Interval:     defaultInterval,
	}
	p := &ipmiProbe{
		BaseProbe:    newBaseProbe(),
		cfg:          cfg,
		moduleLogger: newTestModuleLogger(t),
		runner:       func(_ ipmiConfig) (string, error) { return "", errIpmitoolNotFound },
	}
	p.SetProbeType(ProbeType)
	p.SetName("test-ipmi")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return error on ipmitool failure, got: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected exactly 1 datapoint (senhub.ipmi.up), got %d", len(points))
	}
	if points[0].Name != "senhub.ipmi.up" {
		t.Errorf("expected senhub.ipmi.up, got %q", points[0].Name)
	}
	if points[0].Value != 0 {
		t.Errorf("senhub.ipmi.up = %v, want 0", points[0].Value)
	}
}

// TestCollect_ExcludeNames verifies that the exclude_names regex
// filters sensors by name.
func TestCollect_ExcludeNames(t *testing.T) {
	re, _ := regexp.Compile("FAN.*_MOD")
	cfg := ipmiConfig{
		Mode:         "local",
		IpmitoolPath: defaultIpmitoolPath,
		Interval:     defaultInterval,
		ExcludeNames: []*regexp.Regexp{re},
	}
	output := "FAN_MOD1        | 2800 RPM          | ok\n" +
		"FAN1            | 3000 RPM          | ok\n"

	p := &ipmiProbe{
		BaseProbe:    newBaseProbe(),
		cfg:          cfg,
		moduleLogger: newTestModuleLogger(t),
		runner:       func(_ ipmiConfig) (string, error) { return output, nil },
	}
	p.SetProbeType(ProbeType)
	p.SetName("test-ipmi")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "hardware.component" && tag.Value == "FAN_MOD1" {
				t.Error("FAN_MOD1 should have been excluded but appeared in datapoints")
			}
		}
	}

	fanSeen := false
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "hardware.component" && tag.Value == "FAN1" {
				fanSeen = true
			}
		}
	}
	if !fanSeen {
		t.Error("FAN1 should be present but was not found in datapoints")
	}
}

// TestCollect_SensorStatusFault verifies that a faulted sensor emits
// hardware.sensor.status=0.
func TestCollect_SensorStatusFault(t *testing.T) {
	output := "CPU Temp        | 90 degrees C      | cr\n"
	cfg := ipmiConfig{Mode: "local", IpmitoolPath: defaultIpmitoolPath, Interval: defaultInterval}
	p := &ipmiProbe{
		BaseProbe:    newBaseProbe(),
		cfg:          cfg,
		moduleLogger: newTestModuleLogger(t),
		runner:       func(_ ipmiConfig) (string, error) { return output, nil },
	}
	p.SetProbeType(ProbeType)
	p.SetName("test-ipmi")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "hardware.sensor.status" && dp.Value != 0 {
			t.Errorf("critical sensor should produce hardware.sensor.status=0, got %v", dp.Value)
		}
	}
}

// TestParseConfig_Defaults verifies that an empty config yields the
// documented defaults without error.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig empty: %v", err)
	}
	if cfg.Mode != "local" {
		t.Errorf("default mode: want 'local', got %q", cfg.Mode)
	}
	if cfg.IpmitoolPath != defaultIpmitoolPath {
		t.Errorf("default ipmitool_path: want %q, got %q", defaultIpmitoolPath, cfg.IpmitoolPath)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("default interval: want %v, got %v", defaultInterval, cfg.Interval)
	}
}

// TestParseConfig_RemoteRequiresHost verifies the validation error when
// mode=remote but no host is configured.
func TestParseConfig_RemoteRequiresHost(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"mode": "remote",
	})
	if err == nil {
		t.Fatal("expected error for remote mode without host")
	}
}

// TestParseConfig_InvalidMode verifies that unsupported mode values are
// rejected.
func TestParseConfig_InvalidMode(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"mode": "ssh",
	})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

// TestParseConfig_InvalidExcludeRegex verifies that a bad regex in
// exclude_names is rejected at parse time.
func TestParseConfig_InvalidExcludeRegex(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"sensors": map[string]interface{}{
			"exclude_names": []interface{}{"[invalid"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid exclude_names regex")
	}
}

// TestGetInterval verifies the probe returns the configured interval.
func TestGetInterval(t *testing.T) {
	cfg := ipmiConfig{Interval: 120 * time.Second}
	p := &ipmiProbe{BaseProbe: newBaseProbe(), cfg: cfg}
	if p.GetInterval() != 120*time.Second {
		t.Errorf("GetInterval() = %v, want 120s", p.GetInterval())
	}
}


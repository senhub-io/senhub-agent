package smart

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestSmartProbe(cfg smartConfig, scanOut []byte, deviceMap map[string][]byte) *smartProbe {
	log := testBaseLogger()
	if cfg.ExecTimeout == 0 {
		cfg.ExecTimeout = defaultExecTimeout
	}
	p := &smartProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: logger.NewModuleLogger(log, "probe.smart"),
		execScan: func(_ context.Context, path string, sudo bool) ([]byte, error) {
			return scanOut, nil
		},
		execDevice: func(_ context.Context, path string, sudo bool, device string) ([]byte, error) {
			if out, ok := deviceMap[device]; ok {
				return out, nil
			}
			return []byte(`{}`), nil
		},
	}
	p.SetProbeType(ProbeType)
	p.SetName("smart-test")
	return p
}

// syntheticScan simulates "smartctl --scan --json" output.
const syntheticScan = `{
  "devices": [
    {"name": "/dev/sda", "type": "disk", "protocol": "ATA"},
    {"name": "/dev/nvme0", "type": "disk", "protocol": "NVMe"}
  ]
}`

// syntheticATADevice simulates "smartctl --json -A -H /dev/sda" for a
// healthy SATA drive.
const syntheticATADevice = `{
  "device": {"name": "/dev/sda", "protocol": "ATA"},
  "smart_status": {"passed": true},
  "temperature": {"current": 42},
  "ata_smart_attributes": {
    "table": [
      {"id":   1, "raw": {"value": 0}},
      {"id":   5, "raw": {"value": 3}},
      {"id":   9, "raw": {"value": 12345}},
      {"id": 197, "raw": {"value": 0}},
      {"id": 198, "raw": {"value": 0}}
    ]
  }
}`

// syntheticNVMeDevice simulates "smartctl --json -A -H /dev/nvme0".
const syntheticNVMeDevice = `{
  "device": {"name": "/dev/nvme0", "protocol": "NVMe"},
  "smart_status": {"passed": true},
  "nvme_smart_health_information_log": {
    "available_spare": 90,
    "available_spare_threshold": 10,
    "percentage_used": 5,
    "data_units_read": 1000000,
    "data_units_written": 500000,
    "media_errors": 0,
    "temperature": 38
  }
}`

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.SmartctlPath != defaultSmartctlBin {
		t.Errorf("SmartctlPath = %q, want %q", cfg.SmartctlPath, defaultSmartctlBin)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", cfg.Interval, defaultInterval)
	}
	if cfg.ExecTimeout != defaultExecTimeout {
		t.Errorf("ExecTimeout = %v, want %v", cfg.ExecTimeout, defaultExecTimeout)
	}
	if cfg.UseSudo {
		t.Error("UseSudo should default to false")
	}
	if len(cfg.Devices) != 0 {
		t.Errorf("Devices should be empty by default, got %v", cfg.Devices)
	}
}

func TestParseConfig_ExecTimeout(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"exec_timeout": 30,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.ExecTimeout != 30*time.Second {
		t.Errorf("ExecTimeout = %v, want 30s", cfg.ExecTimeout)
	}
}

func TestParseConfig_ExplicitValues(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"smartctl_path":   "/usr/local/bin/smartctl",
		"use_sudo":        true,
		"interval":        60,
		"devices":         []interface{}{"/dev/sda", "/dev/sdb"},
		"exclude_devices": []interface{}{"/dev/sdb"},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.SmartctlPath != "/usr/local/bin/smartctl" {
		t.Errorf("SmartctlPath = %q", cfg.SmartctlPath)
	}
	if !cfg.UseSudo {
		t.Error("UseSudo should be true")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", cfg.Interval)
	}
	if len(cfg.Devices) != 2 {
		t.Errorf("Devices len = %d, want 2", len(cfg.Devices))
	}
	if !cfg.ExcludeDevices["/dev/sdb"] {
		t.Error("/dev/sdb should be in ExcludeDevices")
	}
}

func TestBuildATAPoints_MetricValues(t *testing.T) {
	p := newTestSmartProbe(
		smartConfig{ExcludeDevices: map[string]bool{}},
		nil, nil,
	)

	var result smartctlOutput
	if err := json.Unmarshal([]byte(syntheticATADevice), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ts := time.Now()
	points := p.buildATAPoints("/dev/sda", result, nil, ts)

	names := make(map[string]float32, len(points))
	for _, dp := range points {
		names[dp.Name] = dp.Value
	}

	cases := []struct {
		metric string
		want   float32
	}{
		{"smart.disk.health", 1},
		{"smart.disk.temperature", 42},
		{"smart.disk.reallocated_sectors", 3},
		{"smart.disk.power_on_hours", 12345},
		{"smart.disk.read_error_rate", 0},
		{"smart.disk.pending_sectors", 0},
		{"smart.disk.uncorrectable_errors", 0},
	}
	for _, c := range cases {
		if v, ok := names[c.metric]; !ok {
			t.Errorf("metric %q not emitted", c.metric)
		} else if v != c.want {
			t.Errorf("metric %q = %v, want %v", c.metric, v, c.want)
		}
	}
}

func TestBuildNVMePoints_MetricValues(t *testing.T) {
	p := newTestSmartProbe(
		smartConfig{ExcludeDevices: map[string]bool{}},
		nil, nil,
	)

	var result smartctlOutput
	if err := json.Unmarshal([]byte(syntheticNVMeDevice), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ts := time.Now()
	points := p.buildNVMePoints("/dev/nvme0", result, nil, ts)

	names := make(map[string]float32, len(points))
	for _, dp := range points {
		names[dp.Name] = dp.Value
	}

	cases := []struct {
		metric string
		want   float32
	}{
		{"smart.disk.health", 1},
		{"smart.nvme.available_spare", 0.9},  // 90% → ratio
		{"smart.nvme.percentage_used", 0.05}, // 5% → ratio
		{"smart.nvme.temperature", 38},
		{"smart.nvme.media_errors", 0},
		{"smart.nvme.data_units_read", 1000000},
		{"smart.nvme.data_units_written", 500000},
	}
	for _, c := range cases {
		if v, ok := names[c.metric]; !ok {
			t.Errorf("NVMe metric %q not emitted", c.metric)
		} else if v != c.want {
			t.Errorf("NVMe metric %q = %v, want %v", c.metric, v, c.want)
		}
	}
}

func TestCollect_DeviceTagPresent(t *testing.T) {
	deviceMap := map[string][]byte{
		"/dev/sda": []byte(syntheticATADevice),
	}
	p := newTestSmartProbe(
		smartConfig{
			Devices:        []string{"/dev/sda"},
			Interval:       300 * time.Second,
			ExcludeDevices: map[string]bool{},
		},
		nil, deviceMap,
	)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints returned")
	}
	for _, dp := range points {
		// probe_type tag must be set by EnrichDataPointsWithProbeName.
		hasProbeType := false
		hasDevice := false
		for _, tag := range dp.Tags {
			if tag.Key == "probe_type" && tag.Value == ProbeType {
				hasProbeType = true
			}
			if tag.Key == "smart.device" {
				hasDevice = true
			}
		}
		if !hasProbeType {
			t.Errorf("datapoint %q missing probe_type tag", dp.Name)
		}
		if !hasDevice {
			t.Errorf("datapoint %q missing smart.device tag", dp.Name)
		}
	}
}

func TestCollect_ExcludeDevices(t *testing.T) {
	deviceMap := map[string][]byte{
		"/dev/sda":   []byte(syntheticATADevice),
		"/dev/nvme0": []byte(syntheticNVMeDevice),
	}
	p := newTestSmartProbe(
		smartConfig{
			Devices:        []string{"/dev/sda", "/dev/nvme0"},
			Interval:       300 * time.Second,
			ExcludeDevices: map[string]bool{"/dev/sda": true},
		},
		nil, deviceMap,
	)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "smart.device" && tag.Value == "/dev/sda" {
				t.Error("excluded device /dev/sda appeared in output")
			}
		}
	}
}

func TestListDevices_AutoScan(t *testing.T) {
	p := newTestSmartProbe(
		smartConfig{ExcludeDevices: map[string]bool{}},
		[]byte(syntheticScan), nil,
	)

	devs, err := p.listDevices()
	if err != nil {
		t.Fatalf("listDevices: %v", err)
	}
	if len(devs) != 2 {
		t.Errorf("expected 2 devices from scan, got %d", len(devs))
	}
}

func TestListDevices_ConfiguredList(t *testing.T) {
	p := newTestSmartProbe(
		smartConfig{
			Devices:        []string{"/dev/sda"},
			ExcludeDevices: map[string]bool{},
		},
		nil, nil,
	)

	devs, err := p.listDevices()
	if err != nil {
		t.Fatalf("listDevices: %v", err)
	}
	if len(devs) != 1 || devs[0] != "/dev/sda" {
		t.Errorf("expected [/dev/sda], got %v", devs)
	}
}

func TestHealthGauge_Failed(t *testing.T) {
	// A drive that failed S.M.A.R.T. must emit health=0.
	failedDevice := `{
	  "device": {"name": "/dev/sdb", "protocol": "ATA"},
	  "smart_status": {"passed": false},
	  "temperature": {"current": 55},
	  "ata_smart_attributes": {"table": []}
	}`
	p := newTestSmartProbe(
		smartConfig{ExcludeDevices: map[string]bool{}},
		nil, nil,
	)
	var result smartctlOutput
	if err := json.Unmarshal([]byte(failedDevice), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	points := p.buildATAPoints("/dev/sdb", result, nil, time.Now())
	for _, dp := range points {
		if dp.Name == "smart.disk.health" {
			if dp.Value != 0 {
				t.Errorf("failed drive health = %v, want 0", dp.Value)
			}
			return
		}
	}
	t.Error("smart.disk.health metric not found")
}

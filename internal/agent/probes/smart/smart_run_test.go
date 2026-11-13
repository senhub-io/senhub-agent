package smart

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

func collectSmartFixturePoints(t *testing.T, cfg smartConfig, scanOut []byte, deviceMap map[string][]byte) []data_store.DataPoint {
	t.Helper()
	if cfg.ExcludeDevices == nil {
		cfg.ExcludeDevices = map[string]bool{}
	}
	p := newTestSmartProbe(cfg, scanOut, deviceMap)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return points
}

func findSmartPointValueByName(points []data_store.DataPoint, name string) (float64, bool) {
	for _, dp := range points {
		if dp.Name == name {
			return dp.Value, true
		}
	}
	return 0, false
}

func findSmartDevicePointValue(points []data_store.DataPoint, device, name string) (float64, bool) {
	for _, dp := range points {
		if dp.Name != name {
			continue
		}
		deviceMatches := false
		for _, tag := range dp.Tags {
			if tag.Key == "smart.device" && tag.Value == device {
				deviceMatches = true
				break
			}
		}
		if deviceMatches {
			return dp.Value, true
		}
	}
	return 0, false
}

func findSmartStateReasonValue(points []data_store.DataPoint, reason string) (float64, bool) {
	for _, dp := range points {
		if dp.Name != "senhub.smart.state" {
			continue
		}
		for _, tag := range dp.Tags {
			if tag.Key == "reason" && tag.Value == reason {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

func findSmartDeviceMetricTag(points []data_store.DataPoint, device, metric, tagKey string) (string, bool) {
	for _, dp := range points {
		if dp.Name != metric {
			continue
		}
		deviceMatches := false
		for _, tag := range dp.Tags {
			if tag.Key == "smart.device" && tag.Value == device {
				deviceMatches = true
				break
			}
		}
		if !deviceMatches {
			continue
		}
		for _, tag := range dp.Tags {
			if tag.Key == tagKey {
				return tag.Value, true
			}
		}
	}
	return "", false
}

func writeShellScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smartctl")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake smartctl script: %v", err)
	}
	return path
}

func TestNewSmartProbe_UsesConfiguredValuesAndLifecycleMethods(t *testing.T) {
	probe, err := NewSmartProbe(map[string]interface{}{
		"smartctl_path":   "/opt/smartctl",
		"use_sudo":        true,
		"interval":        120,
		"exec_timeout":    15,
		"devices":         []interface{}{"/dev/sda", "/dev/sdb"},
		"exclude_devices": []interface{}{"/dev/sdb"},
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewSmartProbe: %v", err)
	}
	sp, ok := probe.(*smartProbe)
	if !ok {
		t.Fatal("NewSmartProbe did not return *smartProbe")
	}

	if got := sp.GetInterval(); got != 120*time.Second {
		t.Errorf("GetInterval() = %v, want 120s", got)
	}
	if !sp.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
	if got := sp.GetTargetStrategies(); !slices.Equal(got, []string{"senhub", "prtg", "http", "otlp"}) {
		t.Errorf("GetTargetStrategies() = %v, want [senhub prtg http otlp]", got)
	}
	if err := sp.OnStart(make(chan struct{})); err != nil {
		t.Errorf("OnStart() = %v, want nil", err)
	}
	if err := sp.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown() = %v, want nil", err)
	}

	if sp.cfg.SmartctlPath != "/opt/smartctl" {
		t.Errorf("cfg.SmartctlPath = %q, want /opt/smartctl", sp.cfg.SmartctlPath)
	}
	if !sp.cfg.UseSudo {
		t.Error("cfg.UseSudo = false, want true")
	}
	if len(sp.cfg.Devices) != 2 {
		t.Errorf("len(cfg.Devices) = %d, want 2", len(sp.cfg.Devices))
	}
	if !sp.cfg.ExcludeDevices["/dev/sdb"] {
		t.Error("cfg.ExcludeDevices does not contain /dev/sdb")
	}
	if sp.cfg.ExecTimeout != 15*time.Second {
		t.Errorf("cfg.ExecTimeout = %v, want 15s", sp.cfg.ExecTimeout)
	}
}

func TestNewSmartProbe_CollectDataPointsCarryProbeTypeSmart(t *testing.T) {
	probe, err := NewSmartProbe(map[string]interface{}{}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewSmartProbe: %v", err)
	}
	sp := probe.(*smartProbe)
	sp.execScan = func(_ context.Context, _ string, _ bool) ([]byte, error) {
		return []byte(syntheticScan), nil
	}
	sp.execDevice = func(_ context.Context, _ string, _ bool, device string) ([]byte, error) {
		switch device {
		case "/dev/sda":
			return []byte(syntheticATADevice), nil
		case "/dev/nvme0":
			return []byte(syntheticNVMeDevice), nil
		default:
			return []byte(`{}`), nil
		}
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}
	for _, dp := range points {
		found := false
		for _, tag := range dp.Tags {
			if tag.Key == "probe_type" && tag.Value == ProbeType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("datapoint %q missing probe_type tag %q", dp.Name, ProbeType)
		}
	}
}

func TestRunScanAndRunDevice_UseFakeSmartctlScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell smartctl script cannot run on windows")
	}
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "args.txt")
	t.Setenv("SMART_FAKE_ARGS_FILE", argsFile)

	script := filepath.Join(tmp, "smartctl")
	body := `#!/bin/sh
printf '%s' "$*" > "$SMART_FAKE_ARGS_FILE"
case "$*" in
  *--scan*) printf 'scan-output' ;;
  *) printf 'device-output' ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake smartctl script: %v", err)
	}

	out, err := runScan(context.Background(), script, false)
	if err != nil {
		t.Fatalf("runScan: %v", err)
	}
	if string(out) != "scan-output" {
		t.Errorf("runScan output = %q, want scan-output", out)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read smartctl args: %v", err)
	}
	if string(args) != "--json --scan" {
		t.Errorf("runScan args = %q, want --json --scan", args)
	}

	out, err = runDevice(context.Background(), script, false, "/dev/nvme0")
	if err != nil {
		t.Fatalf("runDevice: %v", err)
	}
	if string(out) != "device-output" {
		t.Errorf("runDevice output = %q, want device-output", out)
	}
	args, err = os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read smartctl args: %v", err)
	}
	if string(args) != "--json -A -H /dev/nvme0" {
		t.Errorf("runDevice args = %q, want --json -A -H /dev/nvme0", args)
	}
}

func TestRunSmartctl_ExitCodesWithoutCommandErrorBitAreAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell smartctl script cannot run on windows")
	}
	cases := []struct {
		name     string
		exitCode int
		want     string
	}{
		{"exit zero", 0, `{"smart_status":{"passed":true}}`},
		{"exit four is health bitmask", 4, `{"smart_status":{"passed":false}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scriptBody := fmt.Sprintf("printf '%%s' '%s'\nexit %d", tc.want, tc.exitCode)
			script := writeShellScript(t, scriptBody)

			out, err := runSmartctl(context.Background(), script, false, []string{"--json", "--scan"})
			if err != nil {
				t.Fatalf("runSmartctl with exit %d: %v", tc.exitCode, err)
			}
			if string(out) != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestRunSmartctl_CommandLineOrDeviceOpenBitsAreFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell smartctl script cannot run on windows")
	}
	for _, exitCode := range []int{1, 2, 3, 5} {
		t.Run(fmt.Sprintf("exit %d", exitCode), func(t *testing.T) {
			script := writeShellScript(t, fmt.Sprintf("printf '{}'\nexit %d", exitCode))

			_, err := runSmartctl(context.Background(), script, false, []string{"--json", "-A", "-H", "/dev/sda"})
			if err == nil {
				t.Fatalf("exit %d has bit 0 or 1 set and must be reported as a failure", exitCode)
			}
		})
	}
}

func TestRunSmartctl_MissingBinaryReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-smartctl")
	_, err := runSmartctl(context.Background(), path, false, []string{"--json", "--scan"})
	if err == nil {
		t.Fatal("runSmartctl returned nil error for missing binary")
	}
	base := filepath.Base(path)
	if !strings.Contains(err.Error(), base) {
		t.Errorf("error = %q, want it to mention missing binary %q", err, base)
	}
}

func TestRunSmartctl_ContextTimeoutKillsHungInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell smartctl script cannot run on windows")
	}
	script := writeShellScript(t, "sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := runSmartctl(ctx, script, false, []string{"--json", "-A", "-H", "/dev/sda"})
	if err == nil {
		t.Fatal("runSmartctl returned nil error for timed-out invocation")
	}
}

func TestCollect_ATAHealthFailure_LowersHealthAndKeepsStateOK(t *testing.T) {
	failedATA := `{
	  "device": {"name": "/dev/sda", "protocol": "ATA"},
	  "smart_status": {"passed": false},
	  "temperature": {"current": 55},
	  "ata_smart_attributes": {"table": []}
	}`
	points := collectSmartFixturePoints(t,
		smartConfig{Devices: []string{"/dev/sda"}, ExcludeDevices: map[string]bool{}},
		nil,
		map[string][]byte{"/dev/sda": []byte(failedATA)},
	)

	health, ok := findSmartDevicePointValue(points, "/dev/sda", "smart.disk.health")
	if !ok {
		t.Fatal("smart.disk.health not emitted for /dev/sda")
	}
	if health != 0 {
		t.Errorf("smart.disk.health = %v, want 0", health)
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.devices.readable"); !ok || got != 1 {
		t.Errorf("senhub.smart.devices.readable = %v, %v; want 1, true", got, ok)
	}
	if got, ok := findSmartStateReasonValue(points, smartStateOK); !ok || got != 1 {
		t.Errorf("senhub.smart.state[%s] = %v, %v; want 1, true", smartStateOK, got, ok)
	}
}

func TestCollect_NVMeHealthFailure_PublishesParsedMetricsAndHealthZero(t *testing.T) {
	nvmeFailure := `{
	  "device": {"name": "/dev/nvme0", "protocol": "NVMe"},
	  "smart_status": {"passed": false},
	  "nvme_smart_health_information_log": {
	    "critical_warning": 4,
	    "available_spare": 80,
	    "available_spare_threshold": 10,
	    "percentage_used": 20,
	    "data_units_read": 1000,
	    "data_units_written": 2000,
	    "media_errors": 3,
	    "temperature": 41
	  }
	}`
	points := collectSmartFixturePoints(t,
		smartConfig{Devices: []string{"/dev/nvme0"}, ExcludeDevices: map[string]bool{}},
		nil,
		map[string][]byte{"/dev/nvme0": []byte(nvmeFailure)},
	)

	wants := map[string]float64{
		"smart.disk.health":             0,
		"smart.nvme.available_spare":    80,
		"smart.nvme.percentage_used":    20,
		"smart.nvme.data_units_read":    1000,
		"smart.nvme.data_units_written": 2000,
		"smart.nvme.media_errors":       3,
		"smart.nvme.temperature":        41,
	}
	for metric, want := range wants {
		got, ok := findSmartDevicePointValue(points, "/dev/nvme0", metric)
		if !ok {
			t.Errorf("metric %s not emitted for /dev/nvme0", metric)
			continue
		}
		if got != want {
			t.Errorf("metric %s = %v, want %v", metric, got, want)
		}
	}
}

func TestCollect_SCSIWithoutProtocol_FallsBackToATAShapeAndEmitsTemperature(t *testing.T) {
	scsiLike := `{
	  "device": {"name": "/dev/sdb", "type": "scsi"},
	  "smart_status": {"passed": true},
	  "temperature": {"current": 50}
	}`
	points := collectSmartFixturePoints(t,
		smartConfig{Devices: []string{"/dev/sdb"}, ExcludeDevices: map[string]bool{}},
		nil,
		map[string][]byte{"/dev/sdb": []byte(scsiLike)},
	)

	health, ok := findSmartDevicePointValue(points, "/dev/sdb", "smart.disk.health")
	if !ok {
		t.Fatal("smart.disk.health not emitted for /dev/sdb")
	}
	if health != 1 {
		t.Errorf("smart.disk.health = %v, want 1", health)
	}
	smartType, ok := findSmartDeviceMetricTag(points, "/dev/sdb", "smart.disk.health", "smart.type")
	if !ok || smartType != "sata" {
		t.Errorf("smart.type for /dev/sdb = %q, %v; want sata, true (unknown protocol falls back to ATA parser)", smartType, ok)
	}
	temperature, ok := findSmartDevicePointValue(points, "/dev/sdb", "smart.disk.temperature")
	if !ok || temperature != 50 {
		t.Errorf("smart.disk.temperature = %v, %v; want 50, true", temperature, ok)
	}
}

func TestCollect_MalformedJSONSkipsDeviceAndReportsUnreadable(t *testing.T) {
	points := collectSmartFixturePoints(t,
		smartConfig{Devices: []string{"/dev/sda"}, ExcludeDevices: map[string]bool{}},
		nil,
		map[string][]byte{"/dev/sda": []byte(`{"device":{"name":"/dev/sda"}`)},
	)

	if _, ok := findSmartDevicePointValue(points, "/dev/sda", "smart.disk.health"); ok {
		t.Error("malformed JSON must not emit smart.disk.health")
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.devices.found"); !ok || got != 1 {
		t.Errorf("senhub.smart.devices.found = %v, %v; want 1, true", got, ok)
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.devices.readable"); !ok || got != 0 {
		t.Errorf("senhub.smart.devices.readable = %v, %v; want 0, true", got, ok)
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.up"); !ok || got != 0 {
		t.Errorf("senhub.smart.up = %v, %v; want 0, true", got, ok)
	}
	if got, ok := findSmartStateReasonValue(points, smartStateUnreadable); !ok || got != 1 {
		t.Errorf("senhub.smart.state[%s] = %v, %v; want 1, true", smartStateUnreadable, got, ok)
	}
}

func TestCollect_EmptyScan_EmitsNoDevicesState(t *testing.T) {
	points := collectSmartFixturePoints(t,
		smartConfig{ExcludeDevices: map[string]bool{}},
		[]byte(`{"devices":[]}`),
		nil,
	)

	if got, ok := findSmartPointValueByName(points, "senhub.smart.devices.found"); !ok || got != 0 {
		t.Errorf("senhub.smart.devices.found = %v, %v; want 0, true", got, ok)
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.devices.readable"); !ok || got != 0 {
		t.Errorf("senhub.smart.devices.readable = %v, %v; want 0, true", got, ok)
	}
	if got, ok := findSmartPointValueByName(points, "senhub.smart.up"); !ok || got != 0 {
		t.Errorf("senhub.smart.up = %v, %v; want 0, true", got, ok)
	}
	if got, ok := findSmartStateReasonValue(points, smartStateNoDevices); !ok || got != 1 {
		t.Errorf("senhub.smart.state[%s] = %v, %v; want 1, true", smartStateNoDevices, got, ok)
	}
}

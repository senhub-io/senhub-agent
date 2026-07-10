// Package smart implements the free S.M.A.R.T. Disk Health probe: it
// calls smartctl to collect disk health attributes from SATA/SAS and
// NVMe drives.  smartctl (part of smartmontools) is a prerequisite and
// must be installed separately by the operator.
//
// Auto-scan: when devices is empty, "smartctl --scan" enumerates every
// drive detected by the OS — same behaviour as smartmontools' own
// auto-discover.  Individual devices can be excluded via exclude_devices.
//
// Two device families are supported:
//
//	SATA/SAS — parsed from ata_smart_attributes table (attribute IDs
//	            1, 5, 9, 194, 197, 198).
//	NVMe      — parsed from nvme_smart_health_information_log.
//
// Each device emits a "smart.device" tag set so per-disk series are
// distinct in every downstream sink.
package smart

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

const defaultExecTimeout = 10 * time.Second

// ProbeType is the stable technical identifier (license claims,
// transformer file name, discriminant registry key).
const ProbeType = "smart"

const (
	defaultInterval    = 300 * time.Second
	defaultSmartctlBin = "smartctl"

	// SMART attribute IDs for SATA/SAS drives.
	attrRawReadErrorRate     = 1
	attrReallocatedSectorCt  = 5
	attrPowerOnHours         = 9
	attrTemperatureCelsius   = 194
	attrCurrentPendingSector = 197
	attrOfflineUncorrectable = 198
)

// smartConfig holds the validated probe configuration.
type smartConfig struct {
	Devices        []string
	ExcludeDevices map[string]bool
	SmartctlPath   string
	UseSudo        bool
	Interval       time.Duration
	ExecTimeout    time.Duration
}

// scanDevice is one entry returned by "smartctl --scan --json".
type scanDevice struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Protocol string `json:"protocol"`
}

// scanResult wraps the top-level JSON output of "smartctl --scan --json".
type scanResult struct {
	Devices []scanDevice `json:"devices"`
}

// smartctlOutput is the subset of "smartctl -A -H --json" we consume.
type smartctlOutput struct {
	Device struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Type     string `json:"type"`
	} `json:"device"`
	SmartStatus struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current int64 `json:"current"`
	} `json:"temperature"`
	AtaSmartAttributes struct {
		Table []struct {
			ID  int `json:"id"`
			Raw struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	NvmeSmartHealthInformationLog struct {
		AvailableSpare          float64 `json:"available_spare"`
		AvailableSpareThreshold float64 `json:"available_spare_threshold"`
		PercentageUsed          float64 `json:"percentage_used"`
		DataUnitsRead           int64   `json:"data_units_read"`
		DataUnitsWritten        int64   `json:"data_units_written"`
		MediaErrors             int64   `json:"media_errors"`
		Temperature             int64   `json:"temperature"`
	} `json:"nvme_smart_health_information_log"`
}

// smartProbe is the public handle; unexported fields are private state.
type smartProbe struct {
	*types.BaseProbe
	cfg          smartConfig
	moduleLogger *logger.ModuleLogger

	// execScan / execDevice are the injection points for unit tests.
	execScan   func(ctx context.Context, path string, useSudo bool) ([]byte, error)
	execDevice func(ctx context.Context, path string, useSudo bool, device string) ([]byte, error)
}

// NewSmartProbe constructs the S.M.A.R.T. probe.  Config errors surface here.
func NewSmartProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.smart")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &smartProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		execScan:     runScan,
		execDevice:   runDevice,
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

func parseConfig(config map[string]interface{}) (smartConfig, error) {
	cfg := smartConfig{
		SmartctlPath:   defaultSmartctlBin,
		Interval:       defaultInterval,
		ExecTimeout:    defaultExecTimeout,
		ExcludeDevices: map[string]bool{},
	}

	if v, ok := config["smartctl_path"].(string); ok && v != "" {
		cfg.SmartctlPath = v
	}
	if v, ok := config["use_sudo"].(bool); ok {
		cfg.UseSudo = v
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["exec_timeout"].(int); ok && v > 0 {
		cfg.ExecTimeout = time.Duration(v) * time.Second
	}

	switch raw := config["devices"].(type) {
	case nil:
		// auto-scan
	case []interface{}:
		for _, item := range raw {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			cfg.Devices = append(cfg.Devices, s)
		}
	case []string:
		cfg.Devices = raw
	}

	switch raw := config["exclude_devices"].(type) {
	case nil:
	case []interface{}:
		for _, item := range raw {
			s, ok := item.(string)
			if !ok || s == "" {
				continue
			}
			cfg.ExcludeDevices[s] = true
		}
	case []string:
		for _, s := range raw {
			cfg.ExcludeDevices[s] = true
		}
	}

	return cfg, nil
}

func (p *smartProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *smartProbe) ShouldStart() bool          { return true }
func (p *smartProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *smartProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("smartctl_path", p.cfg.SmartctlPath).
		Bool("use_sudo", p.cfg.UseSudo).
		Msg("Starting S.M.A.R.T. Disk Health probe")
	return nil
}

func (p *smartProbe) OnShutdown(_ context.Context) error { return nil }

// Collect enumerates devices (via scan or config), queries each, and
// emits metrics.  A device that fails to respond is logged and skipped;
// it does not abort the whole collection cycle.
func (p *smartProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	hostTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("smart: getting host tags: %w", err)
	}

	// Each smartctl invocation gets its own bounded context so that a
	// hung or unresponsive disk cannot stall the scheduler indefinitely.
	devices, err := p.listDevices()
	if err != nil {
		return nil, fmt.Errorf("smart: listing devices: %w", err)
	}

	var points []data_store.DataPoint
	for _, dev := range devices {
		if p.cfg.ExcludeDevices[dev] {
			continue
		}
		dp, err := p.collectDevice(dev, hostTags, now)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("device", dev).Msg("smart: device query failed; skipping")
			continue
		}
		points = append(points, dp...)
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// newExecContext returns a context with the configured per-invocation
// deadline.  The caller is responsible for calling cancel.
func (p *smartProbe) newExecContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), p.cfg.ExecTimeout)
}

// listDevices returns the configured device list or the auto-scan result.
func (p *smartProbe) listDevices() ([]string, error) {
	if len(p.cfg.Devices) > 0 {
		return p.cfg.Devices, nil
	}
	ctx, cancel := p.newExecContext()
	defer cancel()
	out, err := p.execScan(ctx, p.cfg.SmartctlPath, p.cfg.UseSudo)
	if err != nil {
		return nil, fmt.Errorf("smartctl --scan: %w", err)
	}
	var sr scanResult
	if err := json.Unmarshal(out, &sr); err != nil {
		return nil, fmt.Errorf("parsing smartctl --scan output: %w", err)
	}
	var devs []string
	for _, d := range sr.Devices {
		if d.Name != "" {
			devs = append(devs, d.Name)
		}
	}
	return devs, nil
}

// collectDevice queries one disk and returns its metric datapoints.
func (p *smartProbe) collectDevice(device string, hostTags []tags.Tag, ts time.Time) ([]data_store.DataPoint, error) {
	ctx, cancel := p.newExecContext()
	defer cancel()
	out, err := p.execDevice(ctx, p.cfg.SmartctlPath, p.cfg.UseSudo, device)
	if err != nil {
		return nil, fmt.Errorf("smartctl -A -H %s: %w", device, err)
	}

	var result smartctlOutput
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parsing smartctl output for %s: %w", device, err)
	}

	proto := result.Device.Protocol
	if proto == "" {
		proto = result.Device.Type
	}

	switch proto {
	case "NVMe":
		return p.buildNVMePoints(device, result, hostTags, ts), nil
	default:
		return p.buildATAPoints(device, result, hostTags, ts), nil
	}
}

// buildATAPoints converts SATA/SAS smartctl output into datapoints.
func (p *smartProbe) buildATAPoints(device string, r smartctlOutput, hostTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	base := append([]tags.Tag{
		{Key: "smart.device", Value: device},
		{Key: "smart.type", Value: "sata"},
		{Key: "metric_type", Value: "health"},
	}, hostTags...)

	health := float64(0)
	if r.SmartStatus.Passed {
		health = 1
	}
	points := []data_store.DataPoint{
		{Name: "smart.disk.health", Value: health, Timestamp: ts, Tags: base},
	}

	// Temperature: first try the top-level temperature block (smartctl 7+),
	// fall back to attribute 194.
	if r.Temperature.Current > 0 {
		points = append(points, data_store.DataPoint{
			Name:      "smart.disk.temperature",
			Value:     float64(r.Temperature.Current),
			Timestamp: ts, Tags: base,
		})
	}

	// Decode ATA attribute table.
	attrVals := map[int]int64{}
	for _, row := range r.AtaSmartAttributes.Table {
		attrVals[row.ID] = row.Raw.Value
	}

	if v, ok := attrVals[attrRawReadErrorRate]; ok {
		points = append(points, data_store.DataPoint{
			Name: "smart.disk.read_error_rate", Value: float64(v), Timestamp: ts, Tags: base,
		})
	}
	if v, ok := attrVals[attrReallocatedSectorCt]; ok {
		points = append(points, data_store.DataPoint{
			Name: "smart.disk.reallocated_sectors", Value: float64(v), Timestamp: ts, Tags: base,
		})
	}
	if v, ok := attrVals[attrPowerOnHours]; ok {
		points = append(points, data_store.DataPoint{
			Name: "smart.disk.power_on_hours", Value: float64(v), Timestamp: ts, Tags: base,
		})
	}
	if r.Temperature.Current == 0 {
		if v, ok := attrVals[attrTemperatureCelsius]; ok {
			points = append(points, data_store.DataPoint{
				Name: "smart.disk.temperature", Value: float64(v), Timestamp: ts, Tags: base,
			})
		}
	}
	if v, ok := attrVals[attrCurrentPendingSector]; ok {
		points = append(points, data_store.DataPoint{
			Name: "smart.disk.pending_sectors", Value: float64(v), Timestamp: ts, Tags: base,
		})
	}
	if v, ok := attrVals[attrOfflineUncorrectable]; ok {
		points = append(points, data_store.DataPoint{
			Name: "smart.disk.uncorrectable_errors", Value: float64(v), Timestamp: ts, Tags: base,
		})
	}
	return points
}

// buildNVMePoints converts NVMe smartctl output into datapoints.
func (p *smartProbe) buildNVMePoints(device string, r smartctlOutput, hostTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	base := append([]tags.Tag{
		{Key: "smart.device", Value: device},
		{Key: "smart.type", Value: "nvme"},
		{Key: "metric_type", Value: "health"},
	}, hostTags...)

	health := float64(0)
	if r.SmartStatus.Passed {
		health = 1
	}

	nvme := r.NvmeSmartHealthInformationLog
	return []data_store.DataPoint{
		{Name: "smart.disk.health", Value: health, Timestamp: ts, Tags: base},
		{Name: "smart.nvme.available_spare", Value: float64(nvme.AvailableSpare), Timestamp: ts, Tags: base},
		{Name: "smart.nvme.percentage_used", Value: float64(nvme.PercentageUsed), Timestamp: ts, Tags: base},
		{Name: "smart.nvme.data_units_read", Value: float64(nvme.DataUnitsRead), Timestamp: ts, Tags: base},
		{Name: "smart.nvme.data_units_written", Value: float64(nvme.DataUnitsWritten), Timestamp: ts, Tags: base},
		{Name: "smart.nvme.media_errors", Value: float64(nvme.MediaErrors), Timestamp: ts, Tags: base},
		{Name: "smart.nvme.temperature", Value: float64(nvme.Temperature), Timestamp: ts, Tags: base},
	}
}

// runScan executes "smartctl --json --scan" and returns its raw output.
// This is the production execScan implementation.
func runScan(ctx context.Context, path string, useSudo bool) ([]byte, error) {
	args := []string{"--json", "--scan"}
	return runSmartctl(ctx, path, useSudo, args)
}

// runDevice executes "smartctl --json -A -H <device>" and returns raw output.
// This is the production execDevice implementation.
func runDevice(ctx context.Context, path string, useSudo bool, device string) ([]byte, error) {
	args := []string{"--json", "-A", "-H", device}
	return runSmartctl(ctx, path, useSudo, args)
}

// runSmartctl is the shared exec helper. smartctl exits with a bitmask
// of condition flags (bit 0 = command-line error, bit 1 = device open
// error, bits 2–7 = S.M.A.R.T. condition flags). Bits 2–7 are informational
// about disk health, not exec failures — we accept exit codes where the
// only set bits are 2–7 (i.e. exit code & 3 == 0).
//
// ctx carries the per-invocation deadline set by the probe; a hung disk
// causes the child process to be killed when the deadline expires.
func runSmartctl(ctx context.Context, path string, useSudo bool, args []string) ([]byte, error) {
	var name string
	var fullArgs []string
	if useSudo {
		name = "sudo"
		fullArgs = append([]string{path}, args...)
	} else {
		name = path
		fullArgs = args
	}

	cmd := exec.CommandContext(ctx, name, fullArgs...) //nolint:gosec
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Accept exit codes where bits 0 and 1 are clear: these indicate
			// disk-health conditions only, not execution errors.
			if exitErr.ExitCode()&0x03 == 0 {
				return out, nil
			}
		}
		return out, err
	}
	return out, nil
}

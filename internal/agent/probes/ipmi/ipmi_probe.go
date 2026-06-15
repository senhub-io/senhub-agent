// Package ipmi implements the free ipmi probe: server hardware sensors
// via IPMI / BMC (temperatures, fans, voltages, power supply status).
//
// Implementation: shells out to ipmitool(8) on each collection cycle.
// Linux-only: the ipmitool subprocess relies on /dev/ipmi0 (in-kernel
// OpenIPMI driver) for local mode. Non-Linux builds compile to a stub
// that always returns senhub.ipmi.up=0 and logs a clear explanation.
//
// Why exec (vs go-ipmi or pure-Go RMCP+): no CGO, no extra build deps,
// ipmitool is present on every monitored Linux server that has a BMC.
// The cost is a child process per cycle — acceptable for a 60s interval.
package ipmi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// the licence catalogue.
const ProbeType = "ipmi"

const (
	defaultInterval      = 60 * time.Second
	defaultExecTimeout   = 10 * time.Second
	defaultIpmitoolPath  = "ipmitool"
	defaultIface         = "lanplus"
	metricTypeHardware   = "hardware"
	metricTypeAvailabity = "availability"
)

// ipmiConfig holds the validated probe configuration.
type ipmiConfig struct {
	Mode           string // "local" or "remote"
	RemoteHost     string
	RemoteUser     string
	RemotePassword string
	RemoteIface    string // "lanplus" or "lan"
	IncludeTypes   []string
	ExcludeNames   []*regexp.Regexp
	IpmitoolPath   string
	Interval       time.Duration
	ExecTimeout    time.Duration // maximum wall time for a single ipmitool invocation
}

// sensorRow represents one parsed ipmitool sdr line.
type sensorRow struct {
	name   string
	value  string // raw value text (e.g. "45 degrees C", "3000 RPM", "12.06 Volts")
	status string // "ok", "cr", "nc", "nr", "ns", "na", etc.
}

// ipmiProbe is the IPMI hardware monitoring probe.
type ipmiProbe struct {
	*types.BaseProbe
	cfg          ipmiConfig
	moduleLogger *logger.ModuleLogger

	// runner is the low-level ipmitool execution function. Swapped in
	// tests with a synthetic stub.
	runner ipmitoolRunner
}

// ipmitoolRunner abstracts the exec call so unit tests can inject
// synthetic output without a real ipmitool binary.
type ipmitoolRunner func(cfg ipmiConfig) (string, error)

// NewIpmiProbe constructs the probe. Configuration errors surface here.
func NewIpmiProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.ipmi")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &ipmiProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		runner:       runIpmitool,
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

func parseConfig(config map[string]interface{}) (ipmiConfig, error) {
	cfg := ipmiConfig{
		Mode:         "local",
		RemoteIface:  defaultIface,
		IpmitoolPath: defaultIpmitoolPath,
		Interval:     defaultInterval,
		ExecTimeout:  defaultExecTimeout,
	}

	if v, ok := config["mode"].(string); ok && v != "" {
		if v != "local" && v != "remote" {
			return cfg, fmt.Errorf("ipmi: mode must be 'local' or 'remote', got %q", v)
		}
		cfg.Mode = v
	}

	if remote, ok := config["remote"].(map[string]interface{}); ok {
		if h, ok := remote["host"].(string); ok {
			cfg.RemoteHost = h
		}
		if u, ok := remote["username"].(string); ok {
			cfg.RemoteUser = u
		}
		if p, ok := remote["password"].(string); ok {
			cfg.RemotePassword = p
		}
		if i, ok := remote["interface"].(string); ok && i != "" {
			cfg.RemoteIface = i
		}
	}

	if cfg.Mode == "remote" && cfg.RemoteHost == "" {
		return cfg, fmt.Errorf("ipmi: remote.host is required when mode=remote")
	}

	if sensors, ok := config["sensors"].(map[string]interface{}); ok {
		if raw, ok := sensors["include_types"].([]interface{}); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok && s != "" {
					cfg.IncludeTypes = append(cfg.IncludeTypes, s)
				}
			}
		}
		if raw, ok := sensors["exclude_names"].([]interface{}); ok {
			for _, v := range raw {
				s, ok := v.(string)
				if !ok || s == "" {
					continue
				}
				re, err := regexp.Compile(s)
				if err != nil {
					return cfg, fmt.Errorf("ipmi: invalid exclude_names regex %q: %w", s, err)
				}
				cfg.ExcludeNames = append(cfg.ExcludeNames, re)
			}
		}
	}

	if v, ok := config["ipmitool_path"].(string); ok && v != "" {
		cfg.IpmitoolPath = v
	}

	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	if v, ok := types.IntParam(config, "exec_timeout"); ok && v > 0 {
		cfg.ExecTimeout = time.Duration(v) * time.Second
	}

	return cfg, nil
}

// GetTargetStrategies advertises the standard set of sinks.
func (p *ipmiProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *ipmiProbe) ShouldStart() bool          { return true }
func (p *ipmiProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *ipmiProbe) OnStart(quitChannel chan struct{}) error {
	mode := p.cfg.Mode
	target := "localhost"
	if mode == "remote" {
		target = p.cfg.RemoteHost
	}
	p.moduleLogger.Info().
		Str("mode", mode).
		Str("target", target).
		Str("ipmitool", p.cfg.IpmitoolPath).
		Msg("Starting ipmi probe")
	return nil
}

func (p *ipmiProbe) OnShutdown(_ context.Context) error { return nil }

// Collect runs ipmitool sdr elist full, parses the output and returns
// datapoints. If ipmitool is absent or fails, it emits senhub.ipmi.up=0
// instead of returning an error — unavailability is a measurement.
func (p *ipmiProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	hostTags, err := common.GetHostTags()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("could not resolve host tags; host.id will be absent")
		hostTags = nil
	}

	out, err := p.runner(p.cfg)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("ipmitool failed; emitting ipmi.up=0")
		upTags := append(append([]tags.Tag{}, hostTags...), tags.Tag{Key: "metric_type", Value: metricTypeAvailabity})
		up := data_store.DataPoint{
			Name:      "senhub.ipmi.up",
			Value:     0,
			Timestamp: now,
			Tags:      upTags,
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName([]data_store.DataPoint{up}, p.GetName()), nil
	}

	rows := parseSdrOutput(out)
	var points []data_store.DataPoint
	for _, row := range rows {
		pts := p.rowToDataPoints(row, now, hostTags)
		points = append(points, pts...)
	}
	upTags := append(append([]tags.Tag{}, hostTags...), tags.Tag{Key: "metric_type", Value: metricTypeAvailabity})
	points = append(points, data_store.DataPoint{
		Name:      "senhub.ipmi.up",
		Value:     1,
		Timestamp: now,
		Tags:      upTags,
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// rowToDataPoints converts a parsed sensor row to zero or more datapoints.
// Sensors that are filtered out (include_types / exclude_names) produce
// no datapoints. A sensor with an unrecognised unit produces only the
// status datapoint. hostTags carries host.id and other resource attributes
// so telemetry joins the host entity emitted by the foundation detector.
func (p *ipmiProbe) rowToDataPoints(row sensorRow, now time.Time, hostTags []tags.Tag) []data_store.DataPoint {
	if !p.shouldInclude(row) {
		return nil
	}

	baseTags := append(append([]tags.Tag{}, hostTags...),
		tags.Tag{Key: "hardware.component", Value: row.name},
		tags.Tag{Key: "metric_type", Value: metricTypeHardware},
	)

	statusOk := isStatusOk(row.status)
	var points []data_store.DataPoint

	// hardware.sensor.status — generic ok/fault for every sensor.
	sensorStatus := float64(0)
	if statusOk {
		sensorStatus = 1
	}
	points = append(points, data_store.DataPoint{
		Name:      "hardware.sensor.status",
		Value:     sensorStatus,
		Timestamp: now,
		Tags:      baseTags,
	})

	// Type-specific metrics.
	val, unit, sensorType := parseValueUnit(row.value)
	switch sensorType {
	case "temperature":
		if val != nil {
			points = append(points, data_store.DataPoint{
				Name:      "hardware.temperature",
				Value:     float64(*val),
				Timestamp: now,
				Tags:      baseTags,
			})
		}

	case "fan":
		if val != nil {
			points = append(points, data_store.DataPoint{
				Name:      "hardware.fan.speed",
				Value:     float64(*val),
				Timestamp: now,
				Tags:      baseTags,
			})
		}

	case "voltage":
		if val != nil {
			points = append(points, data_store.DataPoint{
				Name:      "hardware.voltage",
				Value:     float64(*val),
				Timestamp: now,
				Tags:      baseTags,
			})
		}

	case "power_supply":
		psStatus := float64(0)
		if statusOk {
			psStatus = 1
		}
		points = append(points, data_store.DataPoint{
			Name:      "hardware.power_supply.status",
			Value:     psStatus,
			Timestamp: now,
			Tags:      baseTags,
		})
	}

	_ = unit
	return points
}

// shouldInclude applies include_types and exclude_names filters.
func (p *ipmiProbe) shouldInclude(row sensorRow) bool {
	for _, re := range p.cfg.ExcludeNames {
		if re.MatchString(row.name) {
			return false
		}
	}
	if len(p.cfg.IncludeTypes) == 0 {
		return true
	}
	_, _, sensorType := parseValueUnit(row.value)
	// Map our internal type names to the operator-facing type labels.
	typeMap := map[string][]string{
		"temperature":  {"Temperature"},
		"fan":          {"Fan"},
		"voltage":      {"Voltage"},
		"power_supply": {"Power Supply"},
	}
	for _, want := range p.cfg.IncludeTypes {
		for internalType, labels := range typeMap {
			for _, label := range labels {
				if strings.EqualFold(want, label) && sensorType == internalType {
					return true
				}
			}
		}
		// also allow raw internal type names
		if strings.EqualFold(want, sensorType) {
			return true
		}
	}
	return false
}

// parseSdrOutput parses the ipmitool "sdr elist full" format:
//
//	CPU Temp        | 45 degrees C      | ok
//
// Fields are pipe-separated; the probe only uses the first three.
func parseSdrOutput(output string) []sensorRow {
	lines := strings.Split(output, "\n")
	rows := make([]sensorRow, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 3 {
			continue
		}
		row := sensorRow{
			name:   strings.TrimSpace(parts[0]),
			value:  strings.TrimSpace(parts[1]),
			status: strings.TrimSpace(strings.ToLower(parts[2])),
		}
		if row.name == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// parseValueUnit classifies a sensor reading by unit and returns the
// numeric value (nil when not a number or "no reading"), the raw unit
// string, and the sensor type ("temperature", "fan", "voltage",
// "power_supply", or "").
//
// ipmitool sdr format examples:
//
//	"45 degrees C"   → temperature
//	"3000 RPM"       → fan
//	"12.06 Volts"    → voltage
//	"no reading"     → (nil, "", "")
func parseValueUnit(raw string) (*float64, string, string) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "no reading") || raw == "" {
		return nil, "", ""
	}

	// patterns: "<number> <unit...>"
	idx := strings.IndexByte(raw, ' ')
	if idx < 0 {
		return nil, "", ""
	}
	numStr := raw[:idx]
	unitStr := strings.TrimSpace(raw[idx+1:])

	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		// Not a numeric reading — may be a discrete sensor (e.g. "Presence")
		return nil, unitStr, classifyUnit(unitStr)
	}

	sensorType := classifyUnit(unitStr)
	return &v, unitStr, sensorType
}

// classifyUnit maps a unit string to a sensor type.
func classifyUnit(unit string) string {
	u := strings.ToLower(unit)
	switch {
	case strings.Contains(u, "degrees c") || strings.Contains(u, "degrees f") ||
		strings.Contains(u, "celsius") || strings.Contains(u, "fahrenheit"):
		return "temperature"
	case strings.Contains(u, "rpm"):
		return "fan"
	case strings.Contains(u, "volt"):
		return "voltage"
	case strings.Contains(u, "watt") || strings.Contains(u, "amp"):
		return "power_supply"
	default:
		return ""
	}
}

// isStatusOk returns true for "ok" and "nc" (non-critical).
// "cr" (critical) and "nr" (non-recoverable) are fault states.
func isStatusOk(status string) bool {
	switch strings.ToLower(status) {
	case "ok", "nc":
		return true
	default:
		return false
	}
}

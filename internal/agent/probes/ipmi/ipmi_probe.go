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

	// sensorNumber ("0Fh") and entity ("3.1", entity id.instance) come
	// from the `sdr elist` layout only; both are empty in the plain one.
	sensorNumber string
	entity       string

	// displayName is name made unique among the sensors of one listing.
	// Set by disambiguateNames; equal to name when the name is already
	// unique, so a series that never collided keeps its identity.
	displayName string
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

	rows := disambiguateNames(parseSdrOutput(out))
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
	if !p.shouldInclude(row) || hasNoReading(row) {
		return nil
	}

	component := row.displayName
	if component == "" {
		component = row.name
	}
	baseTags := append(append([]tags.Tag{}, hostTags...),
		tags.Tag{Key: "hardware.component", Value: component},
		tags.Tag{Key: "metric_type", Value: metricTypeHardware},
	)
	if component != row.name {
		if row.entity != "" {
			baseTags = append(baseTags, tags.Tag{Key: "hardware.entity", Value: row.entity})
		}
		if row.sensorNumber != "" {
			baseTags = append(baseTags, tags.Tag{Key: "hardware.sensor_number", Value: row.sensorNumber})
		}
	}

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
	val, _, sensorType := parseValueUnit(row.value)
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

	case "power":
		if val != nil {
			points = append(points, data_store.DataPoint{
				Name:      "hardware.power",
				Value:     *val,
				Timestamp: now,
				Tags:      baseTags,
			})
		}

	case "current":
		if val != nil {
			points = append(points, data_store.DataPoint{
				Name:      "senhub.hardware.current",
				Value:     *val,
				Timestamp: now,
				Tags:      baseTags,
			})
		}
	}

	if isPowerSupplyRow(row) {
		psOk := statusOk
		if state, known := discreteState(row.value); known && state == stateBad {
			psOk = false
		}
		psStatus := float64(0)
		if psOk {
			psStatus = 1
		}
		points = append(points, data_store.DataPoint{
			Name:      "hardware.power_supply.status",
			Value:     psStatus,
			Timestamp: now,
			Tags:      baseTags,
		})
	}

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
		"temperature": {"Temperature"},
		"fan":         {"Fan"},
		"voltage":     {"Voltage"},
		"power":       {"Power"},
		"current":     {"Current"},
	}
	for _, want := range p.cfg.IncludeTypes {
		if strings.EqualFold(want, "Power Supply") && isPowerSupplyRow(row) {
			return true
		}
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

// parseSdrOutput parses both layouts ipmitool prints. `sdr elist full`,
// which the probe runs, puts the sensor number and the entity between
// the name and the reading:
//
//	CPU Temp         | 30h | ok  |  3.1 | 45 degrees C
//
// while the plain `sdr` listing has three fields:
//
//	CPU Temp         | 45 degrees C      | ok
//
// Reading the elist line as the plain one took the sensor number for the
// reading, so no temperature, fan or voltage value was ever emitted.
func parseSdrOutput(output string) []sensorRow {
	lines := strings.Split(output, "\n")
	rows := make([]sensorRow, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		var row sensorRow
		switch {
		case len(parts) >= 5 && sensorNumber.MatchString(parts[1]):
			row = sensorRow{name: parts[0], value: parts[4], status: strings.ToLower(parts[2]), sensorNumber: parts[1]}
			if entityID.MatchString(parts[3]) {
				row.entity = parts[3]
			}
		case len(parts) >= 3:
			row = sensorRow{name: parts[0], value: parts[1], status: strings.ToLower(parts[2])}
		default:
			continue
		}
		if row.name == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// entityID is the fourth field of an elist line, "entity id.instance".
var entityID = regexp.MustCompile(`^\d+\.\d+$`)

// sensorNumber is the second field of an elist line, the sensor number
// in hexadecimal ("30h").
var sensorNumber = regexp.MustCompile(`^[0-9A-Fa-f]{1,2}h$`)

// parseValueUnit classifies a sensor reading by unit and returns the
// numeric value (nil when not a number or "no reading"), the raw unit
// string, and the sensor type ("temperature", "fan", "voltage", "power",
// "current", or "").
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
	case strings.Contains(u, "watt"):
		return "power"
	case strings.Contains(u, "amp"):
		return "current"
	default:
		return ""
	}
}

// hasNoReading reports a sensor the BMC has no reading for: "ns" (not
// scanning, the usual answer for an absent fan or an empty PSU bay) or
// "na". It says nothing about the component's health, so it yields no
// point at all: a status of 0 read as a failed component on every sink.
func hasNoReading(row sensorRow) bool {
	switch row.status {
	case "ns", "na":
		return true
	}
	if strings.EqualFold(row.value, "no reading") {
		return true
	}
	state, known := discreteState(row.value)
	return known && state == stateAbsent
}

type discreteKind int

const (
	stateGood discreteKind = iota
	stateBad
	stateAbsent
)

// discreteState classifies the text a discrete sensor prints in place of
// a number. Presence and redundancy are the readings that matter on a
// power supply; known is false for any other text.
func discreteState(raw string) (discreteKind, bool) {
	r := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case r == "":
		return 0, false
	case strings.Contains(r, "absent"), strings.Contains(r, "not present"):
		return stateAbsent, true
	case strings.Contains(r, "lost"), strings.Contains(r, "degraded"),
		strings.Contains(r, "non-redundant"), strings.Contains(r, "failure"),
		strings.Contains(r, "predictive"), strings.Contains(r, "configuration error"):
		return stateBad, true
	case strings.Contains(r, "presence detected"), strings.Contains(r, "device present"),
		strings.Contains(r, "redundant"), strings.Contains(r, "redundancy regained"):
		return stateGood, true
	}
	return 0, false
}

var powerSupplyName = regexp.MustCompile(`(?i)\b(ps\d*|psu\d*|power supply|pwr supply)\b|redundancy`)

// isPowerSupplyRow reports whether a sensor belongs to a power supply:
// by entity when the elist layout gives one (10 power supply, 19 power
// unit, 20 power module), by name otherwise (the plain layout has no
// entity, and redundancy sensors sit on a board). A reading of the whole
// machine ("Pwr Consumption") is not a supply.
func isPowerSupplyRow(row sensorRow) bool {
	id, _, _ := strings.Cut(row.entity, ".")
	switch id {
	case "10", "19", "20":
		return true
	}
	return powerSupplyName.MatchString(row.name)
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

// entityLabels names the IPMI entity ids that tell sensors of the same
// name apart (Dell calls every CPU temperature "Temp"). An id not listed
// is printed as its raw "id.instance".
var entityLabels = map[string]string{
	"3": "CPU", "66": "CPU", "8": "Memory", "32": "Memory", "10": "PSU",
	"19": "Power Unit", "20": "Power Module", "29": "Fan", "30": "Cooling Unit",
	"4": "Disk", "26": "Disk Bay", "40": "Battery", "7": "Board", "55": "Air Inlet",
}

func entityLabel(entity string) string {
	id, instance, ok := strings.Cut(entity, ".")
	if !ok {
		return ""
	}
	if label, known := entityLabels[id]; known {
		return label + " " + instance
	}
	return entity
}

// disambiguateNames gives every sensor a displayName unique within the
// listing. Sensors whose name is already unique keep it untouched. A
// duplicated name gets its entity ("Temp (CPU 1)"); if that still
// collides, the sensor number, then the position, is appended.
func disambiguateNames(rows []sensorRow) []sensorRow {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.name]++
	}
	taken := map[string]bool{}
	for name, n := range counts {
		if n == 1 {
			taken[name] = true
		}
	}
	seen := map[string]int{}
	for i := range rows {
		r := &rows[i]
		r.displayName = r.name
		if counts[r.name] == 1 {
			continue
		}
		seen[r.name]++
		qualifier := entityLabel(r.entity)
		candidate := r.name
		if qualifier != "" {
			candidate = r.name + " (" + qualifier + ")"
		}
		if taken[candidate] || candidate == r.name {
			switch {
			case r.sensorNumber != "" && qualifier != "":
				candidate = r.name + " (" + qualifier + " " + r.sensorNumber + ")"
			case r.sensorNumber != "":
				candidate = r.name + " (" + r.sensorNumber + ")"
			default:
				candidate = r.name + " (" + strconv.Itoa(seen[r.name]) + ")"
			}
		}
		for n := 2; taken[candidate]; n++ {
			candidate = r.name + " (" + strconv.Itoa(n) + ")"
		}
		taken[candidate] = true
		r.displayName = candidate
	}
	return rows
}

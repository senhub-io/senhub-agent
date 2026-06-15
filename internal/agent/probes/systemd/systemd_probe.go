//go:build linux

// Package systemd monitors systemd units via the D-Bus API.
//
// Implementation: connects to the system D-Bus socket and calls
// ListUnits() to enumerate unit states. For services, NRestarts is
// fetched via GetUnitTypeProperty to provide a restart counter.
// Unit selection supports explicit names and shell globs; when the
// units list is empty, all active units matching include_types are
// returned.
//
// Linux-only: the D-Bus systemd manager is a Linux concept. The stub
// (probe_stub.go) returns ErrNotSupported on non-Linux builds.
package systemd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dbus "github.com/coreos/go-systemd/v22/dbus"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the canonical type name used in license claims and the
// transformer file name.
const ProbeType = "systemd"

// defaultIncludeTypes is the set of unit type suffixes enabled when
// include_types is not specified.
var defaultIncludeTypes = []string{"service", "socket", "timer", "mount"}

// probeConfig holds the parsed operator config.
type probeConfig struct {
	Units        []string
	IncludeTypes map[string]bool
	Interval     time.Duration
}

// SystemdProbe collects per-unit active / sub / load state gauges and
// the restart counter for service units.
type SystemdProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	hostname     string
	entitySource *systemdEntitySource
}

// NewSystemdProbe is the constructor registered in registry.go.
func NewSystemdProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.systemd")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	hostname, _ := os.Hostname()

	probe := &SystemdProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		hostname:     hostname,
		entitySource: newEntitySource(hostname),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Interval:     30 * time.Second,
		IncludeTypes: make(map[string]bool),
	}

	// units: explicit list or globs
	if raw, ok := config["units"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				cfg.Units = append(cfg.Units, s)
			}
		}
	}

	// include_types
	if raw, ok := config["include_types"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				cfg.IncludeTypes[strings.ToLower(s)] = true
			}
		}
	}
	if len(cfg.IncludeTypes) == 0 {
		for _, t := range defaultIncludeTypes {
			cfg.IncludeTypes[t] = true
		}
	}

	// interval
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	return cfg, nil
}

func (p *SystemdProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *SystemdProbe) ShouldStart() bool          { return true }
func (p *SystemdProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *SystemdProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("units", p.config.Units).
		Msg("Starting systemd probe")
	return nil
}

func (p *SystemdProbe) OnShutdown(ctx context.Context) error { return nil }

// Collect opens a D-Bus connection, lists units and emits datapoints.
// The connection is opened and closed per cycle: this keeps the probe
// lightweight (no persistent goroutine) and avoids stale connection
// issues after a systemd restart.
func (p *SystemdProbe) Collect() ([]data_store.DataPoint, error) {
	conn, err := dbus.NewSystemConnection()
	if err != nil {
		return nil, fmt.Errorf("connecting to system D-Bus: %w", err)
	}
	defer conn.Close()

	all, err := conn.ListUnits()
	if err != nil {
		return nil, fmt.Errorf("listing systemd units: %w", err)
	}

	selected := p.filterUnits(all)

	// Resolve host identity for the runs_on relation. Best-effort: an
	// empty hostID silently omits the relation rather than failing the
	// collect cycle.
	hostID := ""
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}

	// Feed entity rail with unit names from this cycle.
	names := make([]string, 0, len(selected))
	for _, u := range selected {
		names = append(names, u.Name)
	}
	p.entitySource.setUnits(names, hostID)

	now := time.Now()
	var points []data_store.DataPoint
	for _, u := range selected {
		pts := p.buildDatapoints(conn, u, now)
		points = append(points, pts...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// filterUnits applies the include_types filter and, when units is
// non-empty, restricts to units matching at least one glob pattern.
func (p *SystemdProbe) filterUnits(all []dbus.UnitStatus) []dbus.UnitStatus {
	var out []dbus.UnitStatus
	for _, u := range all {
		unitType := unitTypeSuffix(u.Name)
		if !p.config.IncludeTypes[unitType] {
			continue
		}
		if len(p.config.Units) == 0 {
			out = append(out, u)
			continue
		}
		for _, pattern := range p.config.Units {
			if matched, _ := filepath.Match(pattern, u.Name); matched {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

// buildDatapoints emits the four metrics for one unit.
func (p *SystemdProbe) buildDatapoints(conn *dbus.Conn, u dbus.UnitStatus, ts time.Time) []data_store.DataPoint {
	unitType := unitTypeSuffix(u.Name)
	baseTags := []tags.Tag{
		{Key: "systemd.unit", Value: u.Name},
		{Key: "systemd.unit.type", Value: unitType},
		{Key: "metric_type", Value: "status"},
	}

	// systemd.unit.active_state: 1=active, 0=otherwise
	var activeVal float32
	if u.ActiveState == "active" {
		activeVal = 1
	}
	points := []data_store.DataPoint{
		{Name: "systemd.unit.active_state", Value: activeVal, Timestamp: ts, Tags: baseTags},
	}

	// systemd.unit.sub_state: 1=running|listening, 0=dead|exited
	// Carry sub_state as a tag so the operator can filter.
	subTags := append(append([]tags.Tag(nil), baseTags...), tags.Tag{Key: "sub_state", Value: u.SubState})
	var subVal float32
	switch u.SubState {
	case "running", "listening":
		subVal = 1
	}
	points = append(points, data_store.DataPoint{Name: "systemd.unit.sub_state", Value: subVal, Timestamp: ts, Tags: subTags})

	// systemd.unit.load_state: 1=loaded, 0=not-found|error
	var loadVal float32
	if u.LoadState == "loaded" {
		loadVal = 1
	}
	points = append(points, data_store.DataPoint{Name: "systemd.unit.load_state", Value: loadVal, Timestamp: ts, Tags: baseTags})

	// systemd.unit.restarts: only for service units (NRestarts property)
	if unitType == "service" {
		prop, err := conn.GetUnitTypeProperty(u.Name, "Service", "NRestarts")
		if err == nil {
			var restarts float32
			if v, ok := prop.Value.Value().(uint32); ok {
				restarts = float32(v)
			}
			points = append(points, data_store.DataPoint{Name: "systemd.unit.restarts", Value: restarts, Timestamp: ts, Tags: baseTags})
		}
	}

	return points
}

// unitTypeSuffix extracts the type from the unit name (e.g. "service"
// from "nginx.service", "timer" from "logrotate.timer").
func unitTypeSuffix(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return ""
}

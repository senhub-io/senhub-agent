//go:build linux

// Package systemd monitors systemd units via the D-Bus API.
//
// Implementation: connects to the system D-Bus socket and calls
// ListUnits() to enumerate unit states. For services, NRestarts is
// fetched via GetUnitTypeProperty to provide a restart counter.
// Unit selection supports explicit names and shell globs; when the
// units list is empty, only enabled units matching include_types are
// returned.
//
// Linux-only: the D-Bus systemd manager is a Linux concept. The stub
// (probe_stub.go) returns ErrNotSupported on non-Linux builds.
package systemd

import (
	"context"
	"fmt"
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

	hostname := common.CanonicalHostName()

	probe := &SystemdProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		hostname:     hostname,
		entitySource: newEntitySource(hostname),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySource)
	return probe, nil
}

func (p *SystemdProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *SystemdProbe) ShouldStart() bool          { return true }
func (p *SystemdProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *SystemdProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("units", p.config.Units).
		Str("detail", p.config.Detail).
		Msg("Starting systemd probe")
	return nil
}

func (p *SystemdProbe) OnShutdown(ctx context.Context) error { return nil }

// checkResultPoint is the probe's one check result: whether it could read
// D-Bus at all. It hangs on the host, since a unit's own state is already
// in its series and one result per unit would multiply the series count.
func checkResultPoint(now time.Time, err error) data_store.DataPoint {
	var t []tags.Tag
	if hi, herr := common.GetHostIdentity(); herr == nil && hi.ID != "" {
		t = append(t, tags.Tag{Key: "host.id", Value: hi.ID})
	}
	return buildCheckResultDatapoint(now, err, t)
}

// Collect opens a D-Bus connection, lists units, selects the units to
// report, and emits datapoints. The connection is opened and closed per
// cycle: this keeps the probe lightweight (no persistent goroutine) and
// avoids stale connection issues after a systemd restart.
func (p *SystemdProbe) Collect() ([]data_store.DataPoint, error) {
	conn, err := dbus.NewSystemConnection()
	if err != nil {
		now := time.Now()
		points := []data_store.DataPoint{checkResultPoint(now, err)}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), fmt.Errorf("connecting to system D-Bus: %w", err)
	}
	defer conn.Close()

	all, err := conn.ListUnits()
	if err != nil {
		now := time.Now()
		points := []data_store.DataPoint{checkResultPoint(now, err)}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), fmt.Errorf("listing systemd units: %w", err)
	}

	stateByName := make(map[string]string)
	if len(p.config.Units) == 0 {
		for _, u := range all {
			unitType := unitTypeSuffix(u.Name)
			if !p.config.IncludeTypes[unitType] || isSessionUnit(u.Name) {
				continue
			}
			prop, err := conn.GetUnitProperty(u.Name, "UnitFileState")
			if err != nil {
				// Unreadable UnitFileState means the unit cannot be
				// selected by the default enabled-only policy.
				continue
			}
			if v, ok := prop.Value.Value().(string); ok {
				stateByName[u.Name] = v
			}
		}
	}

	selected := filterUnits(p.config, all, stateByName)

	// Explicit selections bypass the default enabled-only filter, so
	// UnitFileState has not been read for them yet. Read it for every
	// selected unit that is still missing it to fill host.service.start_mode.
	for _, u := range selected {
		if _, ok := stateByName[u.Name]; ok {
			continue
		}
		prop, err := conn.GetUnitProperty(u.Name, "UnitFileState")
		if err != nil {
			continue
		}
		if v, ok := prop.Value.Value().(string); ok {
			stateByName[u.Name] = v
		}
	}

	// Resolve host identity for the runs_on relation and datapoint tags.
	hostID, hostName := "", p.hostname
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
		if hi.Name != "" {
			hostName = hi.Name
		}
	}

	// Feed entity rail with the selected unit snapshots from this cycle.
	units := make([]hostServiceUnit, 0, len(selected))
	for _, u := range selected {
		units = append(units, hostServiceUnit{
			Name:          u.Name,
			ActiveState:   u.ActiveState,
			UnitFileState: stateByName[u.Name],
		})
	}
	p.entitySource.setUnits(units, hostID, hostName)

	now := time.Now()
	var points []data_store.DataPoint
	for _, u := range selected {
		var nRestarts *float64
		if p.config.Detail == systemdDetailFull && unitTypeSuffix(u.Name) == "service" {
			prop, err := conn.GetUnitTypeProperty(u.Name, "Service", "NRestarts")
			if err == nil {
				if v, ok := prop.Value.Value().(uint32); ok {
					r := float64(v)
					nRestarts = &r
				}
			}
		}

		pts := buildDatapoints(u, now, nRestarts, hostID, hostName, p.config.Detail)
		points = append(points, pts...)
	}
	points = append(points, checkResultPoint(now, nil))

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

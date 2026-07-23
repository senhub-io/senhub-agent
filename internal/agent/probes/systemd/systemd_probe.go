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
	"time"

	dbus "github.com/coreos/go-systemd/v22/dbus"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
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

	hostname, _ := os.Hostname()

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

	selected := filterUnits(p.config, all)

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
		var nRestarts *float64
		if unitTypeSuffix(u.Name) == "service" {
			prop, err := conn.GetUnitTypeProperty(u.Name, "Service", "NRestarts")
			if err == nil {
				if v, ok := prop.Value.Value().(uint32); ok {
					r := float64(v)
					nRestarts = &r
				}
			}
		}
		pts := buildDatapoints(u, now, nRestarts)
		points = append(points, pts...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// Package winservices implements the free winservices probe: it reports
// the state of Windows services via the Service Control Manager (SCM)
// API. For a Windows estate the running/stopped state of a named service
// (a SQL Server instance, a Citrix broker, an antivirus agent, a custom
// app service) is a top PRTG sensor; the SCM gives it away natively.
//
// Platform: Windows-only. The SCM enumeration lives in collector_windows.go
// behind a `//go:build windows` tag; collector_other.go provides a stub
// that fails loudly on every other OS, so a single probe definition
// compiles and registers across mixed-OS fleets (same approach as
// windows_eventlog / linux_logs).
//
// Entity rail (#185): the host's service-control surface is reported as a
// single service.instance entity (winservices://localhost) so a backend can
// anchor the per-service metrics to a host. The per-service rows ride as
// metrics (windows.service.state / windows.service.status), not as entities.
package winservices

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the canonical registry / license / transformer name for
// this probe. It is part of license JWT claims and config files in the
// wild — renaming it is a breaking change (see .claude/rules/probes.md).
const ProbeType = "winservices"

// DefaultInterval is the poll cadence used when the config omits interval.
const DefaultInterval = 30 * time.Second

// Service Control Manager states (winsvc SERVICE_STATUS dwCurrentState).
const (
	stateStopped         = 1
	stateStartPending    = 2
	stateStopPending     = 3
	stateRunning         = 4
	stateContinuePending = 5
	statePausePending    = 6
	statePaused          = 7
)

// serviceState is one service's name and SCM state at a point in time.
type serviceState struct {
	name  string
	state int
}

// collectFunc returns the current state of the selected services. The real
// implementation (collector_windows.go) queries the SCM; the stub
// (collector_other.go) returns an error so a non-Windows host fails loudly.
type collectFunc func(selected []string) ([]serviceState, error)

// WinServicesProbeConfig captures the operator-supplied options from the
// probe YAML `params` block.
type WinServicesProbeConfig struct {
	// Services restricts the report to these service names. Empty means
	// every service the SCM enumerates.
	Services []string

	// Interval is the poll cadence. Defaults to DefaultInterval.
	Interval time.Duration
}

// WinServicesProbe is the Windows services state reader.
type WinServicesProbe struct {
	*types.BaseProbe
	config       WinServicesProbeConfig
	moduleLogger *logger.ModuleLogger
	collect      collectFunc

	entitySource           *winServicesEntitySource
	unregisterEntitySource func()
}

// NewWinServicesProbe constructs the probe. Config errors surface here;
// platform availability is checked lazily in Collect via the collectFunc so
// the probe registers on every OS.
func NewWinServicesProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe."+ProbeType)

	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &WinServicesProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       parsed,
		moduleLogger: moduleLogger,
		collect:      collectServices,
		entitySource: newEntitySource(),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (WinServicesProbeConfig, error) {
	parsed := WinServicesProbeConfig{Interval: DefaultInterval}

	parsed.Services = stringSlice(config["services"])

	if d, ok, err := durationSeconds(config["interval"]); err != nil {
		return parsed, fmt.Errorf("winservices: interval: %w", err)
	} else if ok {
		parsed.Interval = d
	}

	return parsed, nil
}

func (p *WinServicesProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *WinServicesProbe) ShouldStart() bool          { return true }
func (p *WinServicesProbe) GetInterval() time.Duration { return p.config.Interval }

// OnStart registers the entity source so the host's service-control surface
// folds into the agent's entity snapshot.
func (p *WinServicesProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().
		Strs("services", p.config.Services).
		Dur("interval", p.config.Interval).
		Msg("Starting winservices probe")
	return nil
}

// OnShutdown unregisters the entity source so a stopped or reloaded probe
// stops heartbeating its cached entity.
func (p *WinServicesProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
}

// Collect queries the SCM for the selected services. A SCM failure is not a
// collection error: the probe emits senhub.winservices.up=0 so the outage is
// observable, mirroring the always-emit-up contract of the other probes.
// Host identity is resolved once per cycle so the entity source can attach
// the runs_on → host relation as soon as the ID is available.
func (p *WinServicesProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	up := float32(1)

	hostID := ""
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}
	p.entitySource.setHostID(hostID)

	states, err := p.collect(p.config.Services)
	if err != nil {
		up = 0
		p.moduleLogger.Warn().Err(err).Msg("winservices collection failed")
	}

	points := []data_store.DataPoint{
		{Name: "senhub.winservices.up", Value: up, Timestamp: now, Tags: statusTags()},
	}
	for _, s := range states {
		points = append(points, p.buildServiceDatapoints(s, now)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *WinServicesProbe) buildServiceDatapoints(s serviceState, ts time.Time) []data_store.DataPoint {
	serviceTags := []tags.Tag{
		{Key: "windows.service.name", Value: s.name},
		{Key: "metric_type", Value: "service"},
	}
	running := float32(0)
	if s.state == stateRunning {
		running = 1
	}
	return []data_store.DataPoint{
		{Name: "windows.service.state", Value: running, Timestamp: ts, Tags: serviceTags},
		{Name: "windows.service.status", Value: float32(s.state), Timestamp: ts, Tags: serviceTags},
	}
}

func statusTags() []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: "status"},
	}
}

func (p *WinServicesProbe) String() string {
	return fmt.Sprintf("WinServicesProbe{services=%v, interval=%s}", p.config.Services, p.config.Interval)
}

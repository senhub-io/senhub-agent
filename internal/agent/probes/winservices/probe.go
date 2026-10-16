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
// Entity rail: the host's service-control surface is reported as a
// service.instance entity (winservices@<host.id>) and each selected service
// is additionally reported as a host.service entity.
package winservices

import (
	"context"
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/hostservice"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the canonical registry / license / transformer name for
// this probe. It is part of license JWT claims and config files in the
// wild — renaming it is a breaking change.
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

// Start types as the Service Control Manager configures them, with one
// addition: an automatic service that starts after the others is reported
// as startAutomaticDelayed, since the delay is a separate flag in the SCM
// and an operator reads "automatic, delayed" as one setting.
const (
	startBoot             = 0
	startSystem           = 1
	startAutomatic        = 2
	startManual           = 3
	startDisabled         = 4
	startAutomaticDelayed = 5
)

// Detail levels available to operators. The default keeps the probe
// cheap in series volume; full restores the pre-detail metric set.
const (
	serviceDetailState = "state"
	serviceDetailFull  = "full"
)

// serviceState is one service's name, SCM state, and SCM configuration at
// a point in time. startType is -1 when the configuration could not be
// read, which leaves the start type unreported rather than guessed.
// triggerStart is nil when trigger info could not be read, otherwise it
// reports whether at least one trigger is configured.
type serviceState struct {
	name         string
	state        int
	startType    int
	startDelayed bool
	triggerStart *bool
}

// collectFunc returns the current state of the selected services. The real
// implementation (collector_windows.go) queries the SCM; the stub
// (collector_other.go) returns an error so a non-Windows host fails loudly.
type collectFunc func(selected []string) ([]serviceState, error)

type hostIDFunc func() string
type hostNameFunc func() string

// WinServicesProbeConfig captures the operator-supplied options from the
// probe YAML `params` block.
type WinServicesProbeConfig struct {
	// Services restricts the report to these service names. Empty means
	// the default automatic-start selection.
	Services []string

	// Detail is "state" (default) for only the per-service running state,
	// or "full" for state, raw status, and start type.
	Detail string

	// Interval is the poll cadence. Defaults to DefaultInterval.
	Interval time.Duration
}

// WinServicesProbe is the Windows services state reader.
type WinServicesProbe struct {
	*types.BaseProbe
	config       WinServicesProbeConfig
	moduleLogger *logger.ModuleLogger
	collect      collectFunc

	entitySource *winServicesEntitySource
	getHostID    hostIDFunc
	getHostName  hostNameFunc

	// reportedMissing holds the selected names already reported as not
	// found, so each is logged once rather than every cycle.
	reportedMissing map[string]bool
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
		getHostID:    defaultHostID,
		getHostName:  defaultHostName,
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySource)
	return probe, nil
}

func defaultHostID() string {
	if hi, err := common.GetHostIdentity(); err == nil {
		return hi.ID
	}
	return ""
}

func defaultHostName() string {
	if hi, err := common.GetHostIdentity(); err == nil && hi.Name != "" {
		return hi.Name
	}
	return common.CanonicalHostName()
}

func parseConfig(config map[string]interface{}) (WinServicesProbeConfig, error) {
	parsed := WinServicesProbeConfig{Interval: DefaultInterval, Detail: serviceDetailState}

	parsed.Services, _ = types.StringSliceParam(config, "services")

	if raw, ok := config["detail"]; ok {
		if s, ok := raw.(string); ok && s != "" {
			if s != serviceDetailState && s != serviceDetailFull {
				return parsed, fmt.Errorf("winservices: detail must be %q or %q, got %q", serviceDetailState, serviceDetailFull, s)
			}
			parsed.Detail = s
		}
	}

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

func (p *WinServicesProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Strs("services", p.config.Services).
		Str("detail", p.config.Detail).
		Dur("interval", p.config.Interval).
		Msg("Starting winservices probe")
	return nil
}

func (p *WinServicesProbe) OnShutdown(_ context.Context) error {
	return nil
}

// selectAutomaticServices is the pure default selection policy for an
// empty services list: only services configured for automatic start,
// including delayed automatic, are selected.
func selectAutomaticServices(states []serviceState) []serviceState {
	out := make([]serviceState, 0, len(states))
	for _, s := range states {
		if s.startType == startAutomatic || s.startType == startAutomaticDelayed {
			out = append(out, s)
		}
	}
	return out
}

// Collect queries the SCM for the selected services. A SCM failure is not a
// collection error: the probe emits senhub.winservices.up=0 so the outage is
// observable, mirroring the always-emit-up contract of the other probes.
func (p *WinServicesProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	up := float64(1)

	hostID := p.getHostID()
	hostName := p.getHostName()
	p.entitySource.setHostID(hostID)

	states, err := p.collect(p.config.Services)
	if err != nil {
		up = 0
		p.moduleLogger.Warn().Err(err).Msg("winservices collection failed")
	}

	if len(p.config.Services) == 0 {
		states = selectAutomaticServices(states)
	}

	if err == nil {
		p.entitySource.setServices(hostID, hostName, states)
	}

	points := []data_store.DataPoint{
		{Name: "senhub.winservices.up", Value: up, Timestamp: now, Tags: p.statusTags()},
		{Name: dbcommon.CheckResultMetric, Value: float64(dbcommon.ClassifyError(err)), Timestamp: now, Tags: p.statusTags()},
	}
	for _, s := range states {
		points = append(points, p.buildServiceDatapoints(s, now, hostID, hostName, p.config.Detail)...)
	}
	if err == nil {
		p.reportMissing(states)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// reportMissing names the selected services the SCM did not report. The
// missing row was the only signal, and a PRTG or Nagios reader cannot see a
// channel that never appeared: a name typed wrong, or a comma-separated
// list saved as one name, went unnoticed.
func (p *WinServicesProbe) reportMissing(states []serviceState) {
	if len(p.config.Services) == 0 {
		return
	}
	seen := make(map[string]bool, len(states))
	for _, s := range states {
		seen[strings.ToLower(s.name)] = true
	}
	for _, want := range p.config.Services {
		if seen[strings.ToLower(want)] || p.reportedMissing[want] {
			continue
		}
		if p.reportedMissing == nil {
			p.reportedMissing = map[string]bool{}
		}
		p.reportedMissing[want] = true
		p.moduleLogger.Warn().Str("service", want).
			Msg("Selected service not found or not readable; it has no state series (check the short name, one per list entry)")
	}
}

func (p *WinServicesProbe) buildServiceDatapoints(s serviceState, ts time.Time, hostID, hostName, detail string) []data_store.DataPoint {
	serviceTags := []tags.Tag{
		{Key: "windows.service.name", Value: s.name},
		{Key: "metric_type", Value: "service"},
	}
	if hostID != "" {
		serviceTags = append(serviceTags,
			tags.Tag{Key: "host.id", Value: hostID},
			tags.Tag{Key: "host.service.name", Value: s.name},
		)
	}
	if hostName != "" {
		serviceTags = append(serviceTags, tags.Tag{Key: "host.name", Value: hostName})
	}

	points := []data_store.DataPoint{
		{Name: hostservice.StateMetric, Value: float64(hostservice.FromWindows(s.state)), Timestamp: ts, Tags: serviceTags},
	}
	if detail != serviceDetailFull {
		return points
	}
	running := float64(0)
	if s.state == stateRunning {
		running = 1
	}
	points = append(points, data_store.DataPoint{Name: "windows.service.state", Value: running, Timestamp: ts, Tags: serviceTags})
	points = append(points, data_store.DataPoint{Name: "windows.service.status", Value: float64(s.state), Timestamp: ts, Tags: serviceTags})
	if s.startType >= 0 {
		points = append(points, data_store.DataPoint{Name: "windows.service.start_type", Value: float64(s.startType), Timestamp: ts, Tags: serviceTags})
	}
	return points
}

func (p *WinServicesProbe) baseTags() []tags.Tag {
	if id := p.entitySource.serviceInstanceID(); id != "" {
		return []tags.Tag{{Key: idKeyServiceInstanceID, Value: id}}
	}
	return nil
}

func (p *WinServicesProbe) statusTags() []tags.Tag {
	t := p.baseTags()
	return append(t, tags.Tag{Key: "metric_type", Value: "status"})
}

func (p *WinServicesProbe) String() string {
	return fmt.Sprintf("WinServicesProbe{services=%v, detail=%s, interval=%s}", p.config.Services, p.config.Detail, p.config.Interval)
}

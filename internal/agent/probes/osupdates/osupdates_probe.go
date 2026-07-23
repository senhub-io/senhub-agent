// Package osupdates implements the free os_updates probe: OS patch
// posture for the machine the agent runs on — pending updates, pending
// security updates and reboot-required status.
//
// The probe queries the native package backend, read-only and without
// privilege escalation:
//
//   - apt (Debian/Ubuntu): /usr/lib/update-notifier/apt-check when
//     present, apt-get -s upgrade as fallback; reboot flag from
//     /var/run/reboot-required.
//   - dnf/yum (RHEL & derivatives): updateinfo list (+ --security);
//     reboot flag via needs-restarting -r.
//   - Windows: Windows Update Agent COM API (Microsoft.Update.Session
//     search + Microsoft.Update.SystemInfo reboot flag).
//
// When the backend is unreachable or the platform is unsupported the
// probe emits senhub.os.updates.up=0 and suppresses the counts for the
// cycle — the series degrades instead of vanishing.
//
// Update status changes slowly; the default interval is 1 hour.
package osupdates

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier (license claims,
// transformer file name, registry key).
const ProbeType = "os_updates"

const (
	defaultInterval       = 3600 * time.Second
	defaultCommandTimeout = 120 * time.Second
)

type osUpdatesConfig struct {
	Interval       time.Duration
	CommandTimeout time.Duration
}

func parseConfig(config map[string]interface{}) osUpdatesConfig {
	cfg := osUpdatesConfig{
		Interval:       defaultInterval,
		CommandTimeout: defaultCommandTimeout,
	}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := config["command_timeout"].(int); ok && v > 0 {
		cfg.CommandTimeout = time.Duration(v) * time.Second
	}
	return cfg
}

// updatesStatus is one backend query result.
type updatesStatus struct {
	pending         int
	pendingSecurity int
	rebootRequired  bool
	packageManager  string // apt | dnf | yum | wua
}

// updatesCollector is the per-platform backend seam. The platform
// selector newOSUpdatesCollector (collector_linux.go /
// collector_windows.go / collector_other.go) picks the implementation.
type updatesCollector interface {
	collect(ctx context.Context) (updatesStatus, error)
}

// OSUpdatesProbe reports the host's patch posture once per interval.
type OSUpdatesProbe struct {
	*types.BaseProbe
	cfg          osUpdatesConfig
	moduleLogger *logger.ModuleLogger
	collector    updatesCollector
}

// NewOSUpdatesProbe is the constructor registered in register.go.
func NewOSUpdatesProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.os_updates")

	p := &OSUpdatesProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          parseConfig(config),
		moduleLogger: moduleLogger,
	}
	p.SetProbeType(ProbeType)
	p.collector = newOSUpdatesCollector(moduleLogger)
	return p, nil
}

func (p *OSUpdatesProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *OSUpdatesProbe) ShouldStart() bool          { return true }
func (p *OSUpdatesProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *OSUpdatesProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Dur("interval", p.cfg.Interval).
		Dur("command_timeout", p.cfg.CommandTimeout).
		Msg("Starting os_updates probe")
	return nil
}

func (p *OSUpdatesProbe) OnShutdown(_ context.Context) error { return nil }

// Collect queries the backend once. On backend failure only
// senhub.os.updates.up=0 is emitted so the availability series stays
// alive while the counts are suppressed for the cycle.
func (p *OSUpdatesProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{{Key: "metric_type", Value: "os_updates"}}

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.CommandTimeout)
	defer cancel()

	status, err := p.collector.collect(ctx)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("os update backend query failed")
		points := []data_store.DataPoint{
			{Name: "senhub.os.updates.up", Value: 0, Timestamp: now, Tags: baseTags},
		}
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
	}

	pointTags := baseTags
	if status.packageManager != "" {
		pointTags = append(append([]tags.Tag{}, baseTags...),
			tags.Tag{Key: "package_manager", Value: status.packageManager})
	}

	rebootValue := float64(0)
	if status.rebootRequired {
		rebootValue = 1
	}

	points := []data_store.DataPoint{
		{Name: "senhub.os.updates.up", Value: 1, Timestamp: now, Tags: pointTags},
		{Name: "senhub.os.updates.pending", Value: float64(status.pending), Timestamp: now, Tags: pointTags},
		{Name: "senhub.os.updates.pending.security", Value: float64(status.pendingSecurity), Timestamp: now, Tags: pointTags},
		{Name: "senhub.os.updates.reboot_required", Value: rebootValue, Timestamp: now, Tags: pointTags},
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *OSUpdatesProbe) String() string {
	return fmt.Sprintf("osUpdatesProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

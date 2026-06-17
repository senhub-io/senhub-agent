// Package linuxlogs reads the local Linux systemd journal and publishes
// log records to the agent's log channel (agentstate.PublishLog).
//
// Implementation: spawns `journalctl --output=json --follow`, parses
// each JSON line into an OTel-shaped LogRecord, and pushes it onto the
// channel. The OTLP strategy (or any future log sink) consumes from
// there.
//
// Why journalctl subprocess (vs sd_journal C bindings or a pure-Go
// reader): no CGO, no extra build deps, works on every modern Linux
// out of the box. The cost is a child process per probe instance —
// negligible for an agent.
//
// Linux-only: on non-Linux builds the OnStart returns an error
// explaining the constraint, and the probe registers but stays inert.
// This lets a single probe definition work in mixed-OS deployments
// without conditional config.
package linuxlogs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// DefaultPriority is the journal priority filter ceiling. 7 = debug;
// 6 = info; 4 = warning; 3 = error. Default 7 means "everything",
// matching `journalctl` defaults.
const DefaultPriority = 7

// LinuxLogsProbeConfig captures the operator-supplied filtering
// options. The probe is a thin wrapper around `journalctl`; every
// option here maps directly to a journalctl flag.
type LinuxLogsProbeConfig struct {
	// Units restricts the journal to specific systemd units. Empty
	// means no unit filter. Each entry becomes a `--unit=<u>` flag.
	Units []string

	// Identifiers filters by SYSLOG_IDENTIFIER (the program name as
	// reported in syslog, like "sshd" or "kernel"). Each entry
	// becomes a `--identifier=<id>` flag.
	Identifiers []string

	// Priority sets the maximum priority emitted (lower priority
	// number = higher severity). 7 = debug+everything above;
	// 4 = warning+errors+critical; etc.
	Priority int

	// IncludeBoot emits records back to the start of the current
	// boot when true. Default false: only new records arriving after
	// the probe starts are shipped — appropriate for a continuous
	// monitoring agent.
	IncludeBoot bool
}

// LinuxLogsProbe is the systemd journal reader probe. It is event-
// driven: `Collect()` always returns nil; the journalctl subprocess
// pushes records onto the agent log channel as they arrive.
type LinuxLogsProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       LinuxLogsProbeConfig
	moduleLogger *logger.ModuleLogger

	// reader is the active journalctl subprocess wrapper. nil before
	// OnStart and after OnShutdown.
	reader *journalReader

	// quitOnce guards the close of the embedded quit channel — Probe
	// pollers may signal shutdown via either OnShutdown(ctx) or by
	// closing the channel passed to OnStart.
	quitOnce sync.Once
}

// NewLinuxLogsProbe constructs the probe. Validation is permissive —
// all config fields are optional; an empty config means "ship every
// record from now on".
func NewLinuxLogsProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.linux_logs")

	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	moduleLogger.Debug().
		Any("config", parsed).
		Msg("Creating new linux_logs probe")

	return &LinuxLogsProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		config:       parsed,
		moduleLogger: moduleLogger,
	}, nil
}

func parseConfig(config map[string]interface{}) (LinuxLogsProbeConfig, error) {
	parsed := LinuxLogsProbeConfig{
		Priority: DefaultPriority,
	}

	if raw, ok := config["units"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				parsed.Units = append(parsed.Units, s)
			}
		}
	}
	if raw, ok := config["identifiers"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				parsed.Identifiers = append(parsed.Identifiers, s)
			}
		}
	}
	if pri, ok := types.IntParam(config, "priority"); ok {
		if pri < 0 || pri > 7 {
			return parsed, fmt.Errorf("priority must be 0..7, got %d", pri)
		}
		parsed.Priority = pri
	}
	if v, ok := config["include_boot"].(bool); ok {
		parsed.IncludeBoot = v
	}

	return parsed, nil
}

// GetTargetStrategies returns an empty list — this probe publishes to
// the agentstate log channel directly, not through the data_store
// router. The OTLP strategy consumes from agentstate and ships via
// otlploggrpc.
func (p *LinuxLogsProbe) GetTargetStrategies() []string {
	return []string{}
}

// ShouldStart always returns true. The probe checks for journalctl
// availability lazily in OnStart; making ShouldStart OS-aware would
// silently disable the probe with no operator feedback when running
// on non-Linux. Better to fail loudly on Start.
func (p *LinuxLogsProbe) ShouldStart() bool {
	return true
}

// GetInterval is irrelevant for an event-driven probe but the poller
// requires a value. We return a long interval — the periodic Collect
// is a no-op anyway.
func (p *LinuxLogsProbe) GetInterval() time.Duration {
	return 5 * time.Minute
}

// Collect is a no-op. The journalctl subprocess pushes records
// directly to the agent log channel as they arrive, independent of
// the poller's tick.
func (p *LinuxLogsProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil
}

// OnStart launches the journalctl subprocess and the goroutine that
// parses its stdout into LogRecords. quitChannel is honored: when
// closed, the subprocess is terminated and the goroutine returns.
func (p *LinuxLogsProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("units", p.config.Units).
		Strs("identifiers", p.config.Identifiers).
		Int("priority", p.config.Priority).
		Bool("include_boot", p.config.IncludeBoot).
		Msg("Starting linux_logs probe")

	reader, err := newJournalReader(p.config, p.moduleLogger, p.GetName())
	if err != nil {
		return fmt.Errorf("start journal reader: %w", err)
	}
	p.reader = reader

	go func() {
		<-quitChannel
		p.moduleLogger.Info().Msg("Quit signal received; stopping journal reader")
		_ = p.reader.stop(context.Background())
	}()

	return nil
}

// OnShutdown is the explicit shutdown entry point used by the agent's
// probe lifecycle manager. Honors the supplied deadline.
func (p *LinuxLogsProbe) OnShutdown(ctx context.Context) error {
	if p.reader == nil {
		return nil
	}
	return p.reader.stop(ctx)
}

// String formats the probe for log statements.
func (p *LinuxLogsProbe) String() string {
	return fmt.Sprintf("LinuxLogsProbe{units=%v, ids=%v, priority=%d}",
		p.config.Units, p.config.Identifiers, p.config.Priority)
}

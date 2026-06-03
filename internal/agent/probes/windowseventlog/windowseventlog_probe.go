// Package windowseventlog reads Windows Event Logs via the native
// wevtapi (EvtSubscribe) and publishes each matching event to the
// agent's log channel (agentstate.PublishLog) as an OTel-shaped
// LogRecord. The OTLP strategy (or any future log sink) consumes from
// there — identical conduit model to the linux_logs probe.
//
// Scope (issue #154):
//   - Read System / Application / Security and custom channels such as
//     "Citrix-XenDesktop-VdaPlugin/Operational".
//   - Filter by channel, level, EventID (include/exclude) and provider
//     glob.
//   - Tail mode (follow new events via EvtSubscribe) and backlog mode
//     (replay from a persisted bookmark on restart).
//   - Persist a per-instance bookmark so restarts neither duplicate nor
//     lose events.
//   - Optional PII-redaction mode for the Security channel (GDPR).
//
// Platform: Windows-only. The wevtapi binding lives in
// subscription_windows.go behind a `//go:build windows` tag. On every
// other OS subscription_other.go provides a stub that fails loudly in
// OnStart, so a single probe definition compiles and registers across
// mixed-OS fleets (same approach as linux_logs).
//
// SCAFFOLD STATUS: the Windows wevtapi subscription, the EvtRender XML
// extraction and the bookmark file format are written but NOT yet
// validated on a real Windows host (see subscription_windows.go and
// docs/probes/windows_eventlog/README.md). The OS-agnostic surface —
// config parsing, event XML parsing, filtering, PII redaction and the
// LogRecord mapping — is complete and unit-tested on any platform.
package windowseventlog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// ProbeType is the canonical registry / license / transformer name for
// this probe. It is part of license JWT claims and config files in the
// wild — renaming it is a breaking change (see .claude/rules/probes.md).
const ProbeType = "windows_eventlog"

// DefaultPollInterval is the bookmark flush / backlog poll cadence used
// when the config omits poll_interval. EvtSubscribe is push-based, so
// this drives bookmark persistence frequency, not event latency.
const DefaultPollInterval = 30 * time.Second

// WindowsEventLogProbeConfig captures the operator-supplied options
// from the probe YAML `params` block (see issue #154 for the schema).
type WindowsEventLogProbeConfig struct {
	// Channels is the list of Event Log channels to subscribe to, e.g.
	// "System", "Application", "Security",
	// "Citrix-XenDesktop-VdaPlugin/Operational". At least one is
	// required.
	Channels []string

	// Levels restricts emitted events by severity label
	// (Critical/Error/Warning/Information/Verbose). Empty means all
	// levels. Parsed into levelInts for the source-side XPath query and
	// the in-process filter.
	Levels    []string
	levelInts []int

	// IncludeEventIDs, when non-empty, is an allow-list: only these
	// EventIDs are emitted. ExcludeEventIDs is always a deny-list and
	// takes precedence (noise suppression, e.g. 4624 logon spam).
	IncludeEventIDs []int
	ExcludeEventIDs []int

	// Sources filters by provider name using shell globs ("Citrix*",
	// "FSLogix*"). Empty means all providers.
	Sources []string

	// PollInterval drives bookmark persistence cadence. Defaults to
	// DefaultPollInterval.
	PollInterval time.Duration

	// BookmarkPath is the file where the subscription bookmark is
	// persisted so the probe resumes without duplication or loss across
	// restarts. Empty disables persistence (tail-from-now each start).
	BookmarkPath string

	// Backlog, when true, replays events from the persisted bookmark (or
	// from the start of each channel when no bookmark exists) before
	// switching to tail mode. Default false: tail new events only.
	Backlog bool

	// RedactPII enables Security-channel PII redaction (GDPR). Sensitive
	// EventData fields and the rendered Security body are blanked.
	RedactPII bool
}

// WindowsEventLogProbe is the Event Log reader probe. Event-driven:
// Collect() always returns nil; the wevtapi subscription pushes records
// onto the agent log channel as they arrive.
type WindowsEventLogProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       WindowsEventLogProbeConfig
	moduleLogger *logger.ModuleLogger

	// reader is the active wevtapi subscription wrapper. nil before
	// OnStart and after OnShutdown. Its concrete type is OS-specific.
	reader *eventReader

	quitOnce sync.Once
}

// NewWindowsEventLogProbe constructs the probe. Returns an error only
// for genuinely invalid config (no channels, bad level name, malformed
// EventID); platform availability is checked lazily in OnStart so the
// probe registers on every OS.
func NewWindowsEventLogProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe."+ProbeType)

	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	moduleLogger.Debug().
		Any("config", parsed).
		Msg("Creating new windows_eventlog probe")

	return &WindowsEventLogProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		config:       parsed,
		moduleLogger: moduleLogger,
	}, nil
}

func parseConfig(config map[string]interface{}) (WindowsEventLogProbeConfig, error) {
	parsed := WindowsEventLogProbeConfig{
		PollInterval: DefaultPollInterval,
	}

	parsed.Channels = stringSlice(config["channels"])
	if len(parsed.Channels) == 0 {
		return parsed, fmt.Errorf("windows_eventlog: at least one channel is required")
	}

	parsed.Levels = stringSlice(config["levels"])
	for _, lvl := range parsed.Levels {
		n, ok := levelTextToInt(lvl)
		if !ok {
			return parsed, fmt.Errorf("windows_eventlog: unknown level %q (want Critical/Error/Warning/Information/Verbose)", lvl)
		}
		parsed.levelInts = append(parsed.levelInts, n)
	}

	var err error
	if parsed.IncludeEventIDs, err = intSlice(config["include_event_ids"]); err != nil {
		return parsed, fmt.Errorf("windows_eventlog: include_event_ids: %w", err)
	}
	if parsed.ExcludeEventIDs, err = intSlice(config["exclude_event_ids"]); err != nil {
		return parsed, fmt.Errorf("windows_eventlog: exclude_event_ids: %w", err)
	}

	parsed.Sources = stringSlice(config["sources"])

	if s, ok := config["bookmark_path"].(string); ok {
		parsed.BookmarkPath = s
	}
	if v, ok := config["backlog"].(bool); ok {
		parsed.Backlog = v
	}
	if v, ok := config["redact_pii"].(bool); ok {
		parsed.RedactPII = v
	}

	if d, ok, err := durationParam(config["poll_interval"]); err != nil {
		return parsed, fmt.Errorf("windows_eventlog: poll_interval: %w", err)
	} else if ok {
		parsed.PollInterval = d
	}

	return parsed, nil
}

// GetTargetStrategies returns an empty list — this probe publishes to
// the agentstate log channel directly, like linux_logs.
func (p *WindowsEventLogProbe) GetTargetStrategies() []string {
	return []string{}
}

// ShouldStart always returns true; the platform check happens in
// OnStart so a non-Windows host fails loudly rather than silently
// disabling the probe.
func (p *WindowsEventLogProbe) ShouldStart() bool {
	return true
}

// GetInterval is the no-op poll cadence; the subscription pushes events
// independently of the poller tick.
func (p *WindowsEventLogProbe) GetInterval() time.Duration {
	return p.config.PollInterval
}

// Collect is a no-op. The wevtapi subscription pushes records onto the
// agent log channel as they arrive.
func (p *WindowsEventLogProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil
}

// OnStart opens the wevtapi subscription and wires the quit channel.
func (p *WindowsEventLogProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Strs("channels", p.config.Channels).
		Strs("levels", p.config.Levels).
		Ints("include_event_ids", p.config.IncludeEventIDs).
		Ints("exclude_event_ids", p.config.ExcludeEventIDs).
		Strs("sources", p.config.Sources).
		Bool("backlog", p.config.Backlog).
		Bool("redact_pii", p.config.RedactPII).
		Str("bookmark_path", p.config.BookmarkPath).
		Msg("Starting windows_eventlog probe")

	reader, err := newEventReader(p.config, p.moduleLogger, p.GetName())
	if err != nil {
		return fmt.Errorf("start windows event log reader: %w", err)
	}
	p.reader = reader

	go func() {
		<-quitChannel
		p.quitOnce.Do(func() {
			p.moduleLogger.Info().Msg("Quit signal received; stopping event log subscription")
			_ = p.reader.stop(context.Background())
		})
	}()

	return nil
}

// OnShutdown closes the subscription and persists the final bookmark.
func (p *WindowsEventLogProbe) OnShutdown(ctx context.Context) error {
	if p.reader == nil {
		return nil
	}
	return p.reader.stop(ctx)
}

// String formats the probe for log statements.
func (p *WindowsEventLogProbe) String() string {
	return fmt.Sprintf("WindowsEventLogProbe{channels=%v, levels=%v, sources=%v}",
		p.config.Channels, p.config.Levels, p.config.Sources)
}

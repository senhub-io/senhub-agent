// Package ibmi implements the IBM i probe for senhub-agent.
//
// At the POC stage this package lives in the senhub4i repository and
// depends on senhub4i.go/pkg/probe for mirror types (Probe interface,
// BaseProbe, DataPoint, ModuleLogger). When absorbed into senhub-agent
// the file will sit at senhub-agent/internal/agent/probes/ibmi/ and the
// imports will be redirected to senhub-agent/internal/agent/probes/types
// etc. — see pkg/probe/datapoint.go for the full mapping.
//
// The probe fronts a JT400 subprocess (internal/probes/ibmi/bridge) that
// holds a persistent JDBC connection to IBM i. Each Collect call iterates
// over a slice of collectors in series, each wrapping one QSYS2 SQL
// Service query, and aggregates the resulting DataPoints plus per-
// collector health metrics.
package ibmi

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// probeType is the technical identifier used in the probe_type tag and
// the ModuleLogger module name. It must stay stable across sprints
// because downstream dashboards filter on it.
const probeType = "ibmi"

// queryExecutor abstracts the JT400 bridge so the probe can be unit-tested
// without spawning a real subprocess. The *bridge.Bridge type satisfies
// this interface natively.
type queryExecutor interface {
	Query(ctx context.Context, sql string) (*bridge.Result, error)
	Close(ctx context.Context) error
}

// ibmiProbe is the concrete Probe implementation.
type ibmiProbe struct {
	*types.BaseProbe

	cfg      probeConfig
	logger   *logger.ModuleLogger
	executor queryExecutor

	collectors      []collector
	collectorStates map[string]*collectorState
	// deltas derives rate/delta series from monotonic counters emitted
	// by individual collectors (e.g. ibmi.job.cpu_time_ms). See
	// counterMetrics below for the current allowlist.
	deltas *deltaStore
}

// counterMetrics lists the metric names whose raw cumulative values
// should also be emitted as "<name>_delta" and "<name>_rate_per_sec"
// by running them through the probe-wide deltaStore. Only monotonic
// counters belong here — gauge-like values (percentages, queue
// depths) would produce meaningless rates.
var counterMetrics = map[string]bool{
	"ibmi.job.cpu_time_ms": true,
}

// defaultCollectors returns the standard list of collectors enabled
// for a freshly-built ibmiProbe. The list grows sprint by sprint as
// new SQL Services are wired in and validated against PUB400.
//
//	Sprint 2: 5 metric collectors (system_status, asp, subsystem,
//	          memory_pool, output_queue)
//	Sprint 3: +2 stateful event collectors (message_queue, history_log)
//	Sprint 4: +4 job & workload collectors (active_job, job_queue,
//	          scheduled_job, msgw_job)
//	Sprint 5: +2 security collectors (user_profile, system_value);
//	          audit_journal is implemented but not registered until
//	          Sprint 8 brings the config-driven opt-in (it requires
//	          *ALLOBJ/*AUDIT authority which PUB400 denies)
//	Sprint 6: +4 network & services collectors (netstat_listener,
//	          netstat_interface, http_server, jvm)
//	Sprint 7: +4 storage/DB/journal collectors (disk_status,
//	          sys_table_stats, journal_info, journal_receiver).
//	Sprint 8: +3 config collectors (library_list, license,
//	          media_library) + enable/disable surface. The
//	          audit_journal collector (requires *ALLOBJ/*AUDIT) is
//	          now wired in but kept out of the default list — opt
//	          in via `enabled_collectors: [..., audit_journal]`
//	          in the probe config.
//	Post-sprint gap closure: +2 collectors (spooled_file,
//	          user_storage) bringing backlog and per-profile
//	          storage into the default set. Two more (ptf,
//	          ptf_group) are wired as opt-in because they require
//	          elevated authority.
//	Coverage push to ~95%: +3 collectors (index_advisor,
//	          query_supervisor, netstat_connection) plus QSYSMSG
//	          parametrization of the message_queue. One more
//	          (authority_collection) wired as opt-in since it
//	          requires CHGAUTCOL to be running.
//	Coverage-final: +1 collector in defaults (hardware_resource —
//	          HARDWARE_RESOURCE_INFO, replaces the non-existent
//	          QSYS2.LINE_INFO). +2 opt-in (service_agent requires
//	          *ALLOBJ, watch_info requires *USE — both verified
//	          against PUB400 PGMR profile).
func defaultCollectors() []collector {
	return []collector{
		systemStatusCollector{},
		aspCollector{},
		subsystemCollector{},
		memoryPoolCollector{},
		outputQueueCollector{},
		activeJobCollector{},
		jobQueueCollector{},
		scheduledJobCollector{},
		userProfileCollector{},
		systemValueCollector{},
		netstatListenerCollector{},
		netstatInterfaceCollector{},
		netstatConnectionCollector{},
		httpServerCollector{},
		jvmCollector{},
		diskStatusCollector{},
		sysTableStatsCollector{},
		journalInfoCollector{},
		journalReceiverCollector{},
		libraryListCollector{},
		licenseCollector{},
		mediaLibraryCollector{},
		spooledFileCollector{},
		userStorageCollector{},
		indexAdvisorCollector{},
		hardwareResourceCollector{},
		newMessageQueueCollector(),
		newHistoryLogCollector(),
		newMsgwJobCollector(),
	}
}

// allKnownCollectors returns every collector implementation the probe
// knows about — including the ones that are not in the default list
// (audit_journal, authority_collection, ptf, ptf_group, service_agent,
// watch_info). Used by the config filter so a deployment can opt into
// disabled-by-default collectors via enabled_collectors.
func allKnownCollectors() []collector {
	defaults := defaultCollectors()
	return append(defaults,
		newAuditJournalCollector(),
		newAuthorityCollectionCollector(),
		newPtfGroupCollector(),
		newPtfCollector(),
		newServiceAgentCollector(),
		newWatchInfoCollector(),
		newQuerySupervisorCollector(),
	)
}

// filterCollectors applies the allowlist/denylist from config to the
// collector set. Semantics:
//
//   - EnabledCollectors empty, DisabledCollectors empty → default set
//   - EnabledCollectors empty, DisabledCollectors non-empty → default
//     set minus denylist
//   - EnabledCollectors non-empty → pick from allKnownCollectors the
//     names listed in EnabledCollectors, then subtract DisabledCollectors
//
// Unknown collector names in either list are silently ignored — this
// is a configuration convenience, not a strictness contract.
func filterCollectors(cfg probeConfig) []collector {
	all := allKnownCollectors()
	byName := make(map[string]collector, len(all))
	for _, c := range all {
		byName[c.Name()] = c
	}

	var source []collector
	if len(cfg.EnabledCollectors) > 0 {
		source = make([]collector, 0, len(cfg.EnabledCollectors))
		for _, name := range cfg.EnabledCollectors {
			if c, ok := byName[name]; ok {
				source = append(source, c)
			}
		}
	} else {
		source = defaultCollectors()
	}

	if len(cfg.DisabledCollectors) == 0 {
		return source
	}
	denylist := make(map[string]bool, len(cfg.DisabledCollectors))
	for _, name := range cfg.DisabledCollectors {
		denylist[name] = true
	}
	out := make([]collector, 0, len(source))
	for _, c := range source {
		if !denylist[c.Name()] {
			out = append(out, c)
		}
	}
	return out
}

// NewIBMiProbe builds an ibmiProbe from the raw configuration map and a
// base logger. It validates the configuration strictly and spawns the
// underlying bridge subprocess eagerly so that a misconfigured probe
// fails at construction time rather than mid-scrape.
//
// The signature mirrors senhub-agent's NewCpuProbe /
// NewXxxProbe(config, *logger.Logger) convention. At integration time,
// logger.Logger becomes logger.Logger and logger.ModuleLogger becomes
// logger.ModuleLogger without any call-site changes.
func NewIBMiProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	if err := platformGate(); err != nil {
		return nil, fmt.Errorf("ibmi probe: %w", err)
	}
	cfg, err := parseProbeConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("ibmi probe: invalid configuration: %w", err)
	}
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe."+probeType)

	br, err := bridge.New(context.Background(), bridge.Config{
		Host:           cfg.Host,
		User:           cfg.User,
		Password:       cfg.Password,
		JavaHome:       cfg.JavaHome,
		NativeRunner:   cfg.NativeRunner,
		RunnerDir:      cfg.BridgeRunnerDir,
		StartupTimeout: cfg.StartupTimeout,
		QueryTimeout:   cfg.QueryTimeout,
	}, moduleLogger.Logger)
	if err != nil {
		return nil, fmt.Errorf("ibmi probe: bridge startup failed: %w", err)
	}

	p := newIbmiProbeWithExecutor(cfg, moduleLogger, br, expandMessageQueues(cfg, filterCollectors(cfg)))
	moduleLogger.Info().
		Str("host", cfg.Host).
		Str("user", cfg.User).
		Dur("interval", cfg.Interval).
		Int("collectors", len(p.collectors)).
		Msg("ibmi probe initialised")
	return p, nil
}

// expandMessageQueues rewrites the base collector set so that the
// historical single `message_queue` collector can be replaced by
// multiple queue-scoped instances driven by the `message_queues`
// config list.
//
// Rules:
//   - If the base list does not contain the default "message_queue"
//     collector (the operator has disabled it), nothing changes.
//   - If cfg.MessageQueues is empty, the base list is returned as-is.
//   - Otherwise the default "message_queue" collector is removed and
//     one `messageQueueCollector` is appended per config entry.
//     Each resulting collector gets a unique Name() — see
//     newMessageQueueCollectorFor.
func expandMessageQueues(cfg probeConfig, base []collector) []collector {
	if len(cfg.MessageQueues) == 0 {
		return base
	}
	filtered := make([]collector, 0, len(base)+len(cfg.MessageQueues))
	foundDefault := false
	for _, c := range base {
		if c.Name() == "message_queue" {
			foundDefault = true
			continue
		}
		filtered = append(filtered, c)
	}
	if !foundDefault {
		return base
	}
	for _, q := range cfg.MessageQueues {
		filtered = append(filtered, newMessageQueueCollectorFor(q.Library, q.Name, q.MinSeverity))
	}
	return filtered
}

// newIbmiProbeWithExecutor is the seam used by tests to inject a fake
// executor and an arbitrary collector list. Keeping it package-private
// avoids advertising a second constructor to production callers.
func newIbmiProbeWithExecutor(cfg probeConfig, logger *logger.ModuleLogger, exec queryExecutor, collectors []collector) *ibmiProbe {
	base := &types.BaseProbe{}
	base.SetName(probeType)
	base.SetProbeType(probeType)

	states := make(map[string]*collectorState, len(collectors))
	for _, c := range collectors {
		states[c.Name()] = &collectorState{}
	}

	return &ibmiProbe{
		BaseProbe:       base,
		cfg:             cfg,
		logger:          logger,
		executor:        exec,
		collectors:      collectors,
		collectorStates: states,
		deltas:          newDeltaStore(),
	}
}

// ShouldStart always returns true — IBM i is reachable from any OS where
// the senhub-agent runs (Linux/macOS/Windows) since everything goes over
// TCP to the JT400 bridge.
func (p *ibmiProbe) ShouldStart() bool { return true }

// GetInterval returns the configured collection period.
func (p *ibmiProbe) GetInterval() time.Duration { return p.cfg.Interval }

// Collect runs every configured collector serially, aggregates the
// DataPoints they produce, and dispatches them to two different
// strategy routers: metric DataPoints (IsEvent=false collectors) go
// through the probe's own GetTargetStrategies (senhub/prtg/http), and
// event DataPoints (IsEvent=true collectors) go through an eventRouter
// that targets the event strategy exclusively. Health DataPoints are
// always routed as metrics.
//
// A collector failing (query error, parse error, context deadline) is
// logged and its health counters are updated, but the remaining
// collectors are still executed — partial results are always preferred
// to all-or-nothing collection.
//
// The return value is the union of both streams enriched with
// probe_name / probe_type tags. Callers that need to distinguish
// metrics from events post-hoc can inspect the "severity" / "message"
// tag presence on each point.
func (p *ibmiProbe) Collect() ([]datapoint.DataPoint, error) {
	// We share a single top-level context across the whole cycle so
	// callers can cancel the probe if the agent is shutting down. Each
	// runCollector imposes its own per-collector deadline within this
	// parent context.
	ctx := context.Background()

	ts := time.Now()
	metricPoints := make([]datapoint.DataPoint, 0, 32)
	eventPoints := make([]datapoint.DataPoint, 0, 8)

	for _, c := range p.collectors {
		state := p.collectorStates[c.Name()]
		points := p.runCollector(ctx, c, state, ts)
		if c.IsEvent() {
			eventPoints = append(eventPoints, points...)
		} else {
			metricPoints = append(metricPoints, points...)
		}
	}

	// Health DataPoints describe the probe itself and always travel on
	// the metric path — an operator wants them available on the regular
	// metrics dashboard, not the event log stream.
	metricPoints = append(metricPoints, p.buildHealthDataPoints(ts)...)

	enrichedMetrics := p.EnrichDataPointsWithProbeName(metricPoints, p.GetName())
	enrichedEvents := p.EnrichDataPointsWithProbeName(eventPoints, p.GetName())

	if p.OnDataPoints != nil {
		// Two dispatches per cycle: metrics go through the probe's
		// own router (senhub/prtg/http), events through a dedicated
		// event-only router. The senhub-agent data_store honours the
		// router argument to decide which strategies receive the
		// batch.
		if len(enrichedMetrics) > 0 {
			if err := p.OnDataPoints(enrichedMetrics, p); err != nil {
				return nil, fmt.Errorf("ibmi probe: OnDataPoints (metrics): %w", err)
			}
		}
		if len(enrichedEvents) > 0 {
			if err := p.OnDataPoints(enrichedEvents, eventRouter{}); err != nil {
				return nil, fmt.Errorf("ibmi probe: OnDataPoints (events): %w", err)
			}
		}
	}

	// Union returned to the caller so Collect's return value stays
	// useful for tests and the demo harness. The dispatch decisions
	// were already made above; this is purely informational.
	return append(enrichedMetrics, enrichedEvents...), nil
}

// OnStart is a no-op: the bridge was already spawned in NewIBMiProbe so
// that a misconfigured probe fails fast at startup.
func (p *ibmiProbe) OnStart(_ chan struct{}) error { return nil }

// OnShutdown tears the bridge subprocess down, honouring the caller's
// context deadline.
func (p *ibmiProbe) OnShutdown(ctx context.Context) error {
	if p.executor == nil {
		return nil
	}
	return p.executor.Close(ctx)
}

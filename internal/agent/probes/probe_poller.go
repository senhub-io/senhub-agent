// Package probes manages metric collection through various probes
package probes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"senhub-agent.go/internal/agent/periodic_scheduler"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// probeTracerScope is the OTel instrumentation scope name for all
// probe.collect spans. Distinct from the otlp strategy scope so a
// consumer can filter traces by source (collection vs export).
const probeTracerScope = "senhub-agent/probes"

// ProbePoller handles the lifecycle and scheduling of a probe.
// It manages initialization, periodic collection, error tracking,
// and shutdown of an individual probe instance.
type ProbePoller struct {
	ProbeId      string                    // Unique identifier for the probe instance
	Probe        types.Probe               // The actual probe implementation
	config       configuration.ProbeConfig // Probe configuration
	addDataPoint data_store.AddCallback    // Callback to store collected data
	moduleLogger *logger.ModuleLogger
	scheduler    periodic_scheduler.PeriodicScheduler
	// unregisterEntitySource removes the probe's entity source from the
	// detector registry. Set in Start, invoked in Shutdown; nil while the
	// probe is not started or when the probe exposes the NoOp fallback.
	unregisterEntitySource func()
	// governanceAttrs is read by the entity source at every Observe, so a
	// governance-only change on reload reaches the next emission without
	// restarting the probe.
	governanceAttrs atomic.Pointer[map[string]any]
	// stopped is set by Shutdown. A collection still blocked when the
	// stop budget ran out returns later; what it brings back belongs to
	// a probe that no longer runs and is dropped.
	stopped atomic.Bool
	// observer reports how the scheduler started each collection.
	observer *scheduleObserver
	// callDeadline is the deadline of each collection's context and
	// collectTimeout the probe's own collect_timeout (zero when unset).
	callDeadline   time.Duration
	collectTimeout time.Duration
}

// probeCadenceSource wraps a probe entity source so it also reports the
// probe's polling interval as its cadence. If the source already knows a
// more specific cadence, the caller keeps that source unwrapped.
type probeCadenceSource struct {
	src      entity.Source
	interval time.Duration
}

func (p probeCadenceSource) Observe() (entity.Observation, bool) {
	return p.src.Observe()
}

func (p probeCadenceSource) Cadence() time.Duration {
	return p.interval
}

func (p probeCadenceSource) Unwrap() entity.Source {
	return p.src
}

// defaultStrategyRouter provides default routing for probes that don't
// implement custom routing. Kept in sync with types.BaseProbe's default so a
// probe reaching this fallback is not silently cut off from otlp/http — every
// signal must be able to reach the OTLP sink (#701). No probe hits this today
// (all embed *types.BaseProbe), but the two defaults must not diverge.
type defaultStrategyRouter struct{}

// GetTargetStrategies returns the default target strategies
func (d *defaultStrategyRouter) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// GenerateProbeId creates a unique identifier for a probe configuration
// by hashing its name and parameters
func GenerateProbeId(config configuration.ProbeConfig) string {
	return config.ID()
}

// NewProbePoller creates and initializes a new probe instance from the given configuration.
// It sets up logging, data collection callback, and probe-specific initialization.
func NewProbePoller(
	config configuration.ProbeConfig,
	baseLogger *logger.Logger,
	addDataPoint data_store.AddCallback,
) (*ProbePoller, error) {
	probeId := GenerateProbeId(config)

	collectTimeout, err := CollectTimeoutOf(config)
	if err != nil {
		return nil, err
	}

	// Create module-specific logger for probe poller using probe type
	// Type is the technical identifier (e.g., "citrix", "cpu"), ensures consistent logging
	probeModuleName := fmt.Sprintf("probe.%s", config.Type)
	moduleLogger := logger.NewModuleLogger(baseLogger, probeModuleName)

	moduleLogger.Debug().Msg("Creating new probe poller")

	probeConstructor, err := getProbeConstructorForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("No constructor for probe %s\n%w", config.Name, err)
	}

	// Said here rather than in each probe: a parameter the probe stopped
	// reading is silently ignored, so the operator's only signal that
	// their option does nothing is this line (#842).
	reportLegacyParams(moduleLogger, config.Name, config.Type, config.Params)

	// A parameter the probe could not read is not a parsing detail: the
	// operator wrote a value and the probe used its default instead.
	// Collected around the construction so the warning can name the
	// probe, which the helpers themselves cannot know (#847).
	var probe types.Probe
	issues := types.CollectParamIssues(func() {
		probe, err = probeConstructor(config.Params, baseLogger)
	})
	for _, issue := range issues {
		moduleLogger.Warn().
			Str("probe_name", config.Name).
			Str("probe_type", config.Type).
			Str("param", issue.Key).
			Interface("value", issue.Got).
			Str("expected", issue.Want).
			Msg("Configured parameter could not be read and was ignored; the probe uses its default instead")
	}
	if err != nil {
		return nil, fmt.Errorf("unable to start probe %s: %w", config.Name, err)
	}

	// Set the unique probe name from configuration (v2 format: name field)
	// This ensures each probe instance has a unique identifier for cache keys
	// All probes that embed BaseProbe will have this method available
	if nameable, ok := probe.(interface{ SetName(string) }); ok {
		nameable.SetName(config.Name)
	} else {
		// Without SetName(), cache keys will not include probe instance name,
		// causing collisions when multiple probe instances exist (e.g., two redfish probes)
		moduleLogger.Warn().
			Str("probe_name", config.Name).
			Str("probe_type", config.Type).
			Msg("Probe does not support SetName() - cache key collisions may occur with multiple probe instances. Probe should embed BaseProbe.")
	}

	// Set the probe type from configuration (v2 format: type field)
	// This is used for discriminant tag lookup in the cache registry
	// All probes that embed BaseProbe will have this method available
	if typeable, ok := probe.(interface{ SetProbeType(string) }); ok {
		typeable.SetProbeType(config.Type)
	} else {
		// Without SetProbeType(), transformer loading and discriminant tag registry lookups will fail,
		// resulting in no metric transformations and incorrect cache key generation
		moduleLogger.Warn().
			Str("probe_name", config.Name).
			Str("probe_type", config.Type).
			Msg("Probe does not support SetProbeType() - transformers and discriminant tags will not work. Probe should embed BaseProbe.")
	}

	// Log routing comes from configuration, not from the probe's metric
	// target list — see BaseProbe.LogTargets for why the two must not be
	// the same list (#836).
	if routable, ok := probe.(interface{ SetLogTargets([]string) }); ok {
		routable.SetLogTargets(config.LogStrategies)
	} else if len(config.LogStrategies) > 0 {
		moduleLogger.Warn().
			Str("probe_name", config.Name).
			Strs("log_strategies", config.LogStrategies).
			Msg("Probe does not support SetLogTargets() - log_strategies will be ignored and its logs will reach every log output. Probe should embed BaseProbe.")
	}

	probePoller := &ProbePoller{
		ProbeId:      probeId,
		Probe:        probe,
		config:       config,
		addDataPoint: addDataPoint,
		moduleLogger: moduleLogger,
	}

	probePoller.collectTimeout = collectTimeout
	probePoller.callDeadline = collectDeadline(collectTimeout, probe.GetInterval())
	probePoller.ApplyGovernance(config)
	probePoller.observer = newScheduleObserver(config.Name, probePoller.probeType())

	// Two probes of one type share the module logger; the scheduler's retry
	// and error lines must say which of them failed.
	schedulerLogger := moduleLogger.With().Str("probe", probe.GetName()).Logger()
	scheduler := periodic_scheduler.NewPeriodicScheduler(periodic_scheduler.PeriodicSchedulerConfig{
		Interval:          probe.GetInterval(),
		InitialDelay:      StartOffset(probeId, probe.GetInterval()),
		MaxRetries:        3,
		ExecuteOnStart:    true,
		ExecuteOnShutdown: false,
		CallTimeout:       probePoller.callDeadline,
		Observer:          probePoller.observer,
		ExecuteContext:    probePoller.collect,
		OnStart:           probe.OnStart,
		OnShutdown:        probe.OnShutdown,
	}, &schedulerLogger)
	probePoller.scheduler = scheduler

	if probeWithCallback, ok := probe.(types.ProbeWithCallback); ok {
		moduleLogger.Debug().Msg("Setting callback for probe")
		probeWithCallback.SetCallback(probePoller.getWrappedCallback())
	}

	moduleLogger.Debug().Msg("Probe poller created successfully")
	return probePoller, nil
}

// getProbeConstructorForConfig retrieves the appropriate constructor function
// for the specified probe type
func getProbeConstructorForConfig(config configuration.ProbeConfig) (ProbeConstructor, error) {
	// Use Type field for constructor lookup (v2 format)
	// Type is the technical identifier (cpu, citrix, redfish, etc.)
	probeType := config.Type
	if probeType == "" {
		return nil, fmt.Errorf("probe type is empty for probe '%s' - configuration should be v2 format", config.Name)
	}

	constructor, exists := probeConstructors[probeType]
	if !exists {
		return nil, fmt.Errorf("unknown probe type: %s (probe name: %s)", probeType, config.Name)
	}
	return constructor, nil
}

// GetName returns the probe's identifier
func (p *ProbePoller) GetName() string {
	return p.Probe.GetName()
}

// GetProbeId returns the unique identifier for this probe instance
func (p *ProbePoller) GetProbeId() string {
	return p.ProbeId
}

// GetProbeParams returns the probe's configuration parameters
func (p *ProbePoller) GetProbeParams() configuration.ProbeConfigParams {
	return p.config.Params
}

// Start begins the periodic collection of metrics from the probe.
// It handles initialization, scheduling, and error recovery. The probe
// runs until ctx is cancelled or Shutdown is called; it never stops
// itself.
func (p *ProbePoller) Start(ctx context.Context) error {
	p.moduleLogger.Debug().Msg("Starting probe")

	if !p.Probe.ShouldStart() {
		p.moduleLogger.Debug().Msg("Probe should not start")
		return nil
	}

	p.stopped.Store(false)
	if err := p.scheduler.Start(ctx); err != nil {
		return err
	}
	p.registerEntitySource()
	return nil
}

// registerEntitySource wires the probe's declared entity source into the
// process-global detector registry. This is the ONLY registration path for
// probe sources: probes declare their source with SetEntitySource in the
// constructor and never call entity.RegisterSource themselves (enforced by
// TestProbePackagesDoNotRegisterEntitySourcesDirectly), so the source the
// registry invariant inspects via EntitySource() is by construction the one
// the detector polls at runtime (#471). The NoOpEntitySource of host-level
// probes and log conduits is skipped — the host entity is already emitted by
// the detector foundation. Registration happens only after a successful
// scheduler start, so a probe whose OnStart failed never heartbeats topology
// it cannot observe.
func (p *ProbePoller) registerEntitySource() {
	if p.unregisterEntitySource != nil {
		return
	}
	src := p.Probe.EntitySource()
	if src == nil {
		return
	}
	if _, isNoOp := src.(types.NoOpEntitySource); isNoOp {
		return
	}
	registered := src
	if !implementsCadenceSource(registered) {
		registered = probeCadenceSource{src: registered, interval: p.Probe.GetInterval()}
	}
	p.unregisterEntitySource = entity.RegisterSource(entity.WithAttributesFunc(registered, p.GovernanceAttributes))
}

// implementsCadenceSource follows Unwrap until it finds a CadenceSource.
func implementsCadenceSource(src entity.Source) bool {
	for src != nil {
		if _, ok := src.(entity.CadenceSource); ok {
			return true
		}
		u, ok := src.(interface{ Unwrap() entity.Source })
		if !ok {
			return false
		}
		src = u.Unwrap()
	}
	return false
}

// ApplyGovernance sets the governance attributes the probe's entities carry
// from cfg. The sensor calls it on every reload for a probe whose identity
// did not change, because governance is not part of that identity. A block
// that does not parse is reported by `config check`; here it is only
// dropped, so a typo in a label never stops a probe from collecting.
func (p *ProbePoller) ApplyGovernance(cfg configuration.ProbeConfig) {
	gov, err := cfg.ParseGovernance()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("governance block ignored")
		p.governanceAttrs.Store(nil)
		return
	}
	attrs := gov.Attributes()
	if len(attrs) == 0 {
		p.governanceAttrs.Store(nil)
		return
	}
	p.governanceAttrs.Store(&attrs)
}

// GovernanceAttributes returns the governance attributes currently stamped
// on the probe's entities, nil when none are declared.
func (p *ProbePoller) GovernanceAttributes() map[string]any {
	if m := p.governanceAttrs.Load(); m != nil {
		return *m
	}
	return nil
}

// collect gathers metrics from the probe and routes them to the appropriate
// storage strategies. It handles both direct collection and callback-based collection.
//
// The collection is wrapped in a "probe.collect" span. When traces are
// disabled the span is a no-op (otel.Tracer resolves to the global noop
// provider). When enabled, span duration captures Collect() latency and
// attributes carry probe identity + emitted datapoints — useful for
// detecting probes that have grown slow or stopped emitting.
func (p *ProbePoller) collect(runCtx context.Context) error {
	p.moduleLogger.Debug().Msg("Collecting data")

	if runCtx == nil {
		runCtx = context.Background()
	}
	tracer := otel.Tracer(probeTracerScope)
	ctx, span := tracer.Start(runCtx, "probe.collect")
	span.SetAttributes(
		attribute.String("probe.name", p.Probe.GetName()),
		attribute.String("probe.type", p.probeType()),
		attribute.String("probe.id", p.ProbeId),
	)
	defer span.End()

	data, err := p.collectOnce(ctx)
	if p.stopped.Load() {
		p.moduleLogger.Debug().Msg("Collection ended after the probe stopped; result dropped")
		return nil
	}
	if err != nil && runCtx.Err() != nil && errors.Is(err, context.Canceled) {
		// The probe is being stopped: an interrupted cycle is not a
		// failure of the target and must not count as one.
		span.SetStatus(codes.Unset, "")
		p.moduleLogger.Debug().Err(err).Msg("Collection cancelled by shutdown")
		return nil
	}
	if err != nil {
		err = p.nameCutDeadline(ctx, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		agentstate.IncrementCollectErrors(p.probeType(), collectErrorReason(err))
		p.recordHealth(err)
		if partial, ok := p.Probe.(types.PartialResultsProbe); ok && partial.KeepsPartialResults() && len(data) > 0 {
			p.routePartial(ctx, data)
			agentstate.RecordProbeCycle(p.ProbeId, len(data))
		}
		return fmt.Errorf("collect failed: %w", err)
	}
	span.SetAttributes(attribute.Int("probe.datapoints_emitted", len(data)))
	span.SetStatus(codes.Ok, "")
	p.recordHealth(nil)
	p.recordDelivery(len(data))

	data = p.withIdentityTags(data)

	if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
		p.moduleLogger.Debug().Msg("Using probe's strategy router")
		return p.addDataPointCtx(ctx, data, strategyRouter)
	}

	p.moduleLogger.Debug().Msg("Using default strategy router")
	return p.addDataPointCtx(ctx, data, &defaultStrategyRouter{})
}

// minCollectDeadline is the shortest default deadline of one collection.
// A collection legitimately longer than its interval (a slow IBM i
// collector, a large SNMP sweep, a slow API) must not lose its data every
// cycle, so a short interval does not shorten the deadline below this.
const minCollectDeadline = 5 * time.Minute

// collectDeadline is the deadline of one collection: the probe's
// collect_timeout when it sets one (shorter or longer than the default),
// else the longer of its interval and minCollectDeadline. Zero means no
// deadline, for a probe without an interval and without a collect_timeout.
func collectDeadline(override, interval time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if interval <= 0 {
		return 0
	}
	return max(interval, minCollectDeadline)
}

// CollectTimeoutOf reads the collect_timeout of a probe entry: absent or
// zero is no override. A value that is neither a duration ("90s") nor a
// number of seconds, or that is negative, is an error naming the key.
func CollectTimeoutOf(config configuration.ProbeConfig) (time.Duration, error) {
	if config.CollectTimeout == nil {
		return 0, nil
	}
	const key = "collect_timeout"
	d, ok := types.DurationParam(map[string]interface{}{key: config.CollectTimeout}, key)
	if !ok {
		return 0, fmt.Errorf("probe %q: %s: want a duration (\"90s\") or a number of seconds, got %v", config.Name, key, config.CollectTimeout)
	}
	if d < 0 {
		return 0, fmt.Errorf("probe %q: %s: must not be negative, got %v", config.Name, key, config.CollectTimeout)
	}
	return d, nil
}

// collectOnce runs one collection: CollectContext under the call context,
// whose deadline the scheduler sets, when the probe can be cancelled,
// Collect as it always was otherwise.
func (p *ProbePoller) collectOnce(ctx context.Context) ([]datapoint.DataPoint, error) {
	if cc, ok := p.Probe.(types.ContextCollector); ok {
		return cc.CollectContext(ctx)
	}
	return p.Probe.Collect()
}

// nameCutDeadline says which setting cut a collection. A bare "context
// deadline exceeded" does not tell the operator that a deadline exists, nor
// which knob moves it. Only the call's own deadline qualifies: a timeout the
// probe applied inside a call that is still alive keeps its own message.
func (p *ProbePoller) nameCutDeadline(ctx context.Context, err error) error {
	if p.callDeadline <= 0 || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	const what = "collection cut at its deadline (collect_timeout, or the interval with a 5 min floor)"
	p.moduleLogger.Warn().
		Dur("deadline", p.callDeadline).
		Dur("collect_timeout", p.collectTimeout).
		Msg(what)
	return fmt.Errorf("%s, deadline %s: %w", what, p.callDeadline, err)
}

// recordDelivery tells the status channel what the cycle delivered. A
// probe whose data arrives through a callback or a listener has an empty
// Collect, and that empty cycle must not erase what the callback last
// delivered.
func (p *ProbePoller) recordDelivery(points int) {
	if points == 0 {
		if _, ok := p.Probe.(types.ProbeWithCallback); ok {
			return
		}
		if _, ok := p.Probe.(types.ListenerProbe); ok {
			return
		}
	}
	agentstate.RecordProbeCycle(p.ProbeId, points)
}

// routePartial routes the datapoints of a failed cycle for a probe that
// declared them valid on their own. A routing failure is logged, not
// returned: the cycle already reports the collect error.
func (p *ProbePoller) routePartial(ctx context.Context, data []datapoint.DataPoint) {
	data = p.withIdentityTags(data)
	var router data_store.StrategyRouter = &defaultStrategyRouter{}
	if r, ok := p.Probe.(data_store.StrategyRouter); ok {
		router = r
	}
	if err := p.addDataPointCtx(ctx, data, router); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("routing the datapoints of a failed cycle failed")
	}
}

// recordHealth publishes the probe's health for this cycle.
//
// For a polling probe that is "did the last Collect succeed". For a
// listener probe it is the state of the listener itself: its Collect
// has nothing to do, so a successful no-op cycle says only that the
// no-op ran. Seven probes reported healthy on that basis while their
// socket could have been closed for hours (#289).
//
// collectErr is the error Collect returned, or nil. A listener probe
// whose Collect failed is unhealthy regardless of what its listener
// says — the failure is real either way.
func (p *ProbePoller) recordHealth(collectErr error) {
	if collectErr != nil {
		p.noteHealth(false, collectErr)
		return
	}

	listener, ok := p.Probe.(types.ListenerProbe)
	if !ok {
		p.noteHealth(true, nil)
		return
	}

	if err := listener.ListenerHealth(); err != nil {
		p.moduleLogger.Warn().
			Err(err).
			Str("probe_name", p.Probe.GetName()).
			Msg("listener is not able to receive; probe reported unhealthy")
		agentstate.IncrementCollectErrors(p.probeType(), "listener")
		p.noteHealth(false, err)
		return
	}
	p.noteHealth(true, nil)
}

// noteHealth publishes the health and turns a transition into an event
// the console lists: the first failure after healthy cycles, and the
// first healthy cycle after failures. Steady states stay quiet.
func (p *ProbePoller) noteHealth(ok bool, cause error) {
	if !ok && cause != nil {
		agentstate.RecordProbeError(p.ProbeId, cause.Error())
	}
	switch agentstate.RecordProbeHealth(p.ProbeId, ok) {
	case "failed":
		msg := "collect failed"
		if cause != nil {
			msg += ": " + cause.Error()
		}
		agentstate.RecordEvent(agentstate.EventError, agentstate.EventKindProbe, p.Probe.GetName(), msg)
	case "recovered":
		agentstate.RecordEvent(agentstate.EventInfo, agentstate.EventKindProbe, p.Probe.GetName(), "collecting again")
	}
}

// withIdentityTags guarantees every datapoint leaving a probe carries
// probe_name and probe_type, whoever produced it.
//
// Probes are expected to call EnrichDataPointsWithProbeName themselves,
// and most do — but "expected to" is the problem: a probe that forgets
// ships untagged datapoints, which reach the cache with a colliding key
// and the sinks with no way to tell one instance from another, silently.
// Adding the tags here makes the guarantee structural instead of
// conventional (#289).
//
// It adds only what is missing, so a probe that already enriched is
// untouched and no tag is ever duplicated. That is what lets the
// probe-side calls be removed later, one probe at a time, without a
// flag day across two repositories.
func (p *ProbePoller) withIdentityTags(data []datapoint.DataPoint) []datapoint.DataPoint {
	probeName := p.Probe.GetName()
	probeType := p.probeType()

	for i := range data {
		var hasName, hasType bool
		for _, t := range data[i].Tags {
			switch t.Key {
			case "probe_name":
				hasName = true
			case "probe_type":
				hasType = true
			}
		}
		if hasName && hasType {
			continue
		}

		// Copy before appending: the probe may hold the slice, and a
		// shared backing array would let one datapoint's append clobber
		// the next one's tags.
		enriched := append([]tags.Tag{}, data[i].Tags...)
		if !hasName {
			enriched = append(enriched, tags.Tag{Key: "probe_name", Value: probeName})
		}
		if !hasType {
			enriched = append(enriched, tags.Tag{Key: "probe_type", Value: probeType})
		}
		data[i].Tags = enriched
	}
	return data
}

// addDataPointCtx is a thin wrapper that exists so future code can
// thread ctx (and thus span context) into the data_store callback if
// it grows a context-aware variant. Today the callback is non-context
// based, so we just call it and ignore ctx — but the call site reads
// naturally and the ctx parameter is non-trivial to remove later.
func (p *ProbePoller) addDataPointCtx(ctx context.Context, data []datapoint.DataPoint, router data_store.StrategyRouter) error {
	_ = ctx
	return p.addDataPoint(data, router)
}

// probeType returns the probe's technical type ("cpu", "redfish", …)
// when the probe embeds BaseProbe; falls back to "unknown" otherwise.
// Used to tag spans without hard-coding a non-existent interface
// method on the Probe interface.
func (p *ProbePoller) probeType() string {
	if t, ok := p.Probe.(interface{ GetProbeType() string }); ok {
		return t.GetProbeType()
	}
	return "unknown"
}

// collectErrorReason classifies a Probe.Collect() error into the bounded
// `reason` label of senhub.agent.collect.errors (#646). It never returns a
// raw error string: a deadline/timeout maps to "timeout", everything else to
// "collect". The routing-failure path passes "route" directly.
func collectErrorReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "collect"
}

// getWrappedCallback returns a function that handles routing of collected data
// to appropriate storage strategies for callback-based probes (syslog, event).
//
// Each successful callback is treated as evidence the probe is alive: we
// publish ok=true to agentstate so senhub_agent_probes_healthy reflects
// callback-driven probes too. A routing failure publishes ok=false, mirroring
// the scheduler-driven path. Note this only proves "the agent received and
// stored a datapoint at least once recently" — it does NOT detect a silent
// listener that has stopped receiving traffic. Operators relying on the
// metric for socket-level health should pair it with an external probe.
func (p *ProbePoller) getWrappedCallback() func([]datapoint.DataPoint) error {
	return func(data []datapoint.DataPoint) error {
		p.moduleLogger.Debug().Int("datapoints_count", len(data)).Msg("Callback triggered")

		data = p.withIdentityTags(data)
		p.recordDelivery(len(data))

		var err error
		if strategyRouter, ok := p.Probe.(data_store.StrategyRouter); ok {
			err = p.addDataPoint(data, strategyRouter)
		} else {
			err = p.addDataPoint(data, &defaultStrategyRouter{})
		}
		if err != nil {
			agentstate.IncrementCollectErrors(p.probeType(), "route")
			p.noteHealth(false, err)
		} else {
			p.recordHealth(nil)
		}
		return err
	}
}

// Shutdown gracefully stops the probe and cleans up resources. The entity
// source is unregistered first so the detector stops polling a probe that is
// tearing down its connections — leaving it registered would heartbeat the
// cached topology of a stopped probe forever (dead targets never expire in
// the consumer, reloads duplicate sources).
func (p *ProbePoller) Shutdown(ctx context.Context) error {
	p.moduleLogger.Debug().Msg("Shutting down probe")
	p.stopped.Store(true)

	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
		p.unregisterEntitySource = nil
	}
	return p.scheduler.Shutdown(ctx)
}

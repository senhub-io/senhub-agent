package otlp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/agentmetrics"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/hostnet"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// strategyName is the canonical identifier used in the data_store
// `endpoints:` configuration. Matches the operator-facing storage name.
const strategyName = "otlp"

// OTLPSyncStrategy implements data_store.SyncStrategy by exporting
// metrics and logs over OTLP/gRPC.
//
//   - Phase 1: lifecycle skeleton (config, gRPC client, shutdown)
//   - Phase 2 (this commit): metrics export — periodic push from a
//     strategy-local LWW store, resolved through otelmapper
//   - Phase 3: logs export from syslog + event probes
type OTLPSyncStrategy struct {
	agentConfig configuration.AgentConfiguration
	rawParams   map[string]interface{}
	cfg         Config
	logger      *logger.ModuleLogger

	// store holds the most recent value of every series flowing through
	// AddDataPoints; the push goroutine snapshots and ships it.
	store *metricStore

	// registry resolves probe types to YAML definitions consumed by
	// otelmapper.Resolve. Each strategy carries its own — definitions
	// are read-only after load and cheap enough to duplicate.
	registry *transformers.TransformerRegistry

	// resource is the OTel Resource (service.name, service.instance.id,
	// service.version, deployment.environment, operator extras, plus the
	// agent-level global_tags) attached to every emitted batch.
	resource *resource.Resource

	// globalTagKeys is the set of agent-level global_tag keys. They are
	// carried on the Resource, so they are stripped from per-metric
	// attributes before export to avoid duplicating them on every series
	// (issue #202).
	globalTagKeys map[string]bool

	// startTime is the OTel `start_time_unix_nano` for cumulative
	// counters. Pinned at strategy.Start so all counters share the same
	// reference (standard OTel pattern — counter resets on agent
	// restart, which the consumer handles).
	startTime time.Time

	exporters *exporters

	// logs holds the SDK BatchProcessor + LoggerProvider + Logger
	// when Logs.Enabled. nil otherwise.
	logs     *logsPipeline
	logsPump *logsPump

	// logsQueue is the on-disk dead-letter queue for the logs signal,
	// set when persistence is enabled and logs are emitted (#217).
	logsQueue *logsQueue

	// entityPump emits entity/relation events on the OTLP log signal; the
	// entity Detector goroutine produces them. Both nil/zero unless
	// Entities.Enabled. entityDetectorCancel stops the detector,
	// entityDetectorWG waits for it.
	entityPump           *entityPump
	entityDetectorCancel context.CancelFunc
	entityDetectorWG     sync.WaitGroup

	// traces holds the SDK BatchSpanProcessor + TracerProvider + Tracer
	// when Traces.Enabled. nil otherwise. The provider also gets
	// registered as the OTel global, so any code that resolves a tracer
	// via otel.Tracer() reaches this exporter.
	traces *tracesPipeline

	// pushTicker drives the metrics push cadence. nil before Start, nil
	// after Shutdown.
	pushTicker *time.Ticker
	// pushDone signals the push goroutine to exit. Closed by Shutdown.
	pushDone chan struct{}
	// pushWG tracks the push goroutine for clean Shutdown.
	pushWG sync.WaitGroup

	// memLimiter polls the Go heap and sets a pressure flag the store
	// reads on the hot path. nil when both thresholds are disabled in
	// the YAML (cfg.MemoryLimit.SoftMiB == 0 && HardMiB == 0).
	memLimiter *memoryLimiter

	// chkpt persists the LWW store to disk periodically and restores
	// it at boot. nil when cfg.Persistence.Path is empty.
	chkpt *checkpointer

	startMu  sync.Mutex
	started  bool
	shutdown bool

	// missingMappingWarned dedups the "metric has no OTel mapping"
	// warning. Same idea as the prometheus side — keyed by
	// "probe_type:metric_name" — so a single misconfigured probe does
	// not flood logs on every push tick.
	missingMappingWarned sync.Map
}

// NewOTLPSyncStrategy constructs (but does not start) the OTLP strategy.
// Returns an interface{} for symmetry with the existing strategy
// constructors that the data_store factory invokes.
//
// The constructor never blocks on the network; gRPC dialing is lazy and
// happens only on the first export attempt.
func NewOTLPSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	params configuration.StorageConfigParams,
	baseLogger *logger.Logger,
) interface{} {
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.otlp")

	cfg, err := ParseConfig(params)
	if err != nil {
		// Log here for visibility during startup; ValidateConfigParams
		// will be called by the data_store right after construction
		// and will surface the same error to the caller.
		moduleLogger.Warn().Err(err).Msg("OTLP config parse failed at construction; ValidateConfigParams will report it")
	}

	// Default service.instance.id from the agent key if the operator
	// didn't override it. Avoids leaking the full key — first 8 chars
	// give enough disambiguation for fleet views without exposing the
	// authentication secret to observability backends.
	if cfg.Resource.ServiceInstance == "" {
		key := agentConfig.GetAuthenticationKey()
		if len(key) > 8 {
			cfg.Resource.ServiceInstance = key[:8]
		} else {
			cfg.Resource.ServiceInstance = key
		}
	}

	// Build optional memory limiter from config. Pass it to the store
	// so the upsert hot path can check its state without taking a
	// reference to the strategy.
	var ml *memoryLimiter
	if cfg.MemoryLimit.SoftMiB > 0 || cfg.MemoryLimit.HardMiB > 0 {
		soft := uint64(cfg.MemoryLimit.SoftMiB) * 1024 * 1024
		hard := uint64(cfg.MemoryLimit.HardMiB) * 1024 * 1024
		ml = newMemoryLimiter(soft, hard, cfg.MemoryLimit.CheckInterval)
	}

	store := newMetricStoreWithCap(cfg.MaxStoreSize)
	if cfg.MaxActiveSeriesPerProbe > 0 {
		store = store.withProbeBudget(cfg.MaxActiveSeriesPerProbe)
	}
	if ml != nil {
		store = store.withMemoryLimiter(ml)
	}

	globalTags := agentConfig.GetGlobalTags()
	globalTagKeys := make(map[string]bool, len(globalTags))
	for k := range globalTags {
		globalTagKeys[k] = true
	}

	s := &OTLPSyncStrategy{
		agentConfig:   agentConfig,
		rawParams:     params,
		cfg:           cfg,
		logger:        moduleLogger,
		store:         store,
		registry:      transformers.NewTransformerRegistry(baseLogger),
		resource:      buildResource(cfg.Resource, cliArgs.Version, globalTags),
		globalTagKeys: globalTagKeys,
		memLimiter:    ml,
	}

	if cfg.Persistence.Path != "" {
		s.chkpt = newCheckpointer(checkpointConfig{
			Path:     cfg.Persistence.Path,
			Interval: cfg.Persistence.Interval,
		}, store, moduleLogger)
	}

	return s
}

// GetStrategyName returns the strategy identifier used by the data_store
// router to match against probe `endpoints:` lists.
func (s *OTLPSyncStrategy) GetStrategyName() string {
	return strategyName
}

// GetStrategyParams returns the raw parameter map this strategy was
// constructed with — used by the data_store to detect configuration
// changes and recreate the strategy when they happen.
func (s *OTLPSyncStrategy) GetStrategyParams() map[string]interface{} {
	return s.rawParams
}

// ValidateConfigParams re-runs ParseConfig on the provided params. The
// data_store calls this right after construction — by re-parsing rather
// than caching the constructor's result, we surface validation errors
// even when the Storage list is replaced via remote config refresh.
func (s *OTLPSyncStrategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	_, err := ParseConfig(params)
	return err
}

// Start brings the strategy online: builds the OTel SDK exporters and,
// when metrics are enabled, launches the periodic push goroutine.
// Idempotent — subsequent calls are no-ops while running. Once Shutdown
// has been called, Start returns an error rather than silently restarting.
func (s *OTLPSyncStrategy) Start() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if s.started {
		return nil
	}
	if s.shutdown {
		return fmt.Errorf("strategy already shut down — cannot restart")
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()

	exp, err := buildExporters(ctx, s.cfg, s.logger)
	if err != nil {
		return fmt.Errorf("build exporters: %w", err)
	}
	s.exporters = exp
	s.startTime = time.Now()

	// Start the memory limiter poller before the metrics pusher so
	// the first cycle's upsert path sees an accurate state flag.
	if s.memLimiter != nil {
		s.memLimiter.start(context.Background())
	}

	// Restore the LWW store from the on-disk checkpoint (if any)
	// before the first push so the consumer sees continuity across
	// restarts. Restore is best-effort: corrupt or missing files
	// start with an empty store and log a warn, no boot failure.
	if s.chkpt != nil {
		if n, err := s.chkpt.loadAndRestore(); err != nil {
			s.logger.Warn().Err(err).Msg("OTLP checkpoint restore failed; starting with empty store")
		} else if n > 0 {
			s.logger.Info().Int("entries", n).Msg("OTLP checkpoint restored")
		}
		s.chkpt.start(context.Background())
	}

	if s.cfg.Metrics.Enabled {
		s.startMetricsPusher()
	}

	// Durable dead-letter queue for the logs signal (#217): wrap the log
	// exporter so a failed export persists event-log records to disk for
	// replay at boot and on backend recovery. Only when persistence is on
	// and raw logs are emitted; entity events are a re-emitted state
	// stream and are not queued.
	var logExp *persistentLogExporter
	if s.cfg.Persistence.Path != "" && s.cfg.Logs.Enabled && s.exporters.log != nil {
		s.logsQueue = newLogsQueue(s.cfg.Persistence.Path, s.cfg.Persistence.LogsQueueMaxBytes, s.logger)
		logExp = newPersistentLogExporter(s.exporters.log, s.logsQueue, s.logger)
		s.exporters.log = logExp
	}

	// Entity events ride the log signal, so the pipeline (provider + both
	// loggers) is built when either logs or entities are enabled.
	if (s.cfg.Logs.Enabled || s.cfg.Entities.Enabled) && s.exporters.log != nil {
		s.logs = buildLogsPipeline(s.exporters.log, s.resource, s.cfg.Logs, cliArgs.Version)
	}

	// Wire queue replay to the pipeline: drain at boot and whenever the
	// backend recovers from a failed export.
	if logExp != nil && s.logs != nil {
		rp := newLogsReplayer(s.logsQueue, s.logs, s.logger)
		logExp.setOnRecovered(rp.replay)
		go rp.replay()
	}

	if s.cfg.Logs.Enabled && s.logs != nil {
		s.logsPump = newLogsPump(s.logs, s.cfg.Logs.BufferSize)
		s.logsPump.start()
	}
	if s.cfg.Entities.Enabled && s.logs != nil {
		s.startEntityEmission()
	}

	if s.cfg.Traces.Enabled && s.exporters.trace != nil {
		s.traces = buildTracesPipeline(s.exporters.trace, s.resource, s.cfg.Traces, cliArgs.Version)
	}

	s.logger.Info().
		Bool("memory_limit_enabled", s.memLimiter != nil && s.memLimiter.enabled()).
		Int("memory_limit_soft_mib", s.cfg.MemoryLimit.SoftMiB).
		Int("memory_limit_hard_mib", s.cfg.MemoryLimit.HardMiB).
		Int("max_store_size", s.cfg.MaxStoreSize).
		Bool("persistence_enabled", s.chkpt != nil && s.chkpt.enabled()).
		Str("persistence_path", s.cfg.Persistence.Path).
		Dur("persistence_interval", s.cfg.Persistence.Interval).
		Str("endpoint", s.cfg.Endpoint).
		Str("protocol", s.cfg.Protocol).
		Bool("tls_enabled", s.cfg.TLS.Enabled).
		Bool("metrics_enabled", s.cfg.Metrics.Enabled).
		Str("metrics_endpoint", s.cfg.Metrics.ResolveEndpoint(s.cfg.Endpoint)).
		Dur("metrics_interval", s.cfg.Metrics.Interval).
		Bool("logs_enabled", s.cfg.Logs.Enabled).
		Str("logs_endpoint", s.cfg.Logs.ResolveEndpoint(s.cfg.Endpoint)).
		Int("logs_batch_size", s.cfg.Logs.BatchSize).
		Dur("logs_batch_timeout", s.cfg.Logs.BatchTimeout).
		Bool("traces_enabled", s.cfg.Traces.Enabled).
		Str("traces_endpoint", s.cfg.Traces.ResolveEndpoint(s.cfg.Endpoint)).
		Float64("traces_sample_ratio", s.cfg.Traces.SampleRatio).
		Str("compression", s.cfg.Compression).
		Dur("timeout", s.cfg.Timeout).
		Msg("OTLP strategy started")

	s.started = true
	return nil
}

// startMetricsPusher launches the periodic push goroutine. Caller must
// hold startMu.
func (s *OTLPSyncStrategy) startMetricsPusher() {
	s.pushTicker = time.NewTicker(s.cfg.Metrics.Interval)
	s.pushDone = make(chan struct{})
	s.pushWG.Add(1)
	go func() {
		defer s.pushWG.Done()
		for {
			select {
			case <-s.pushDone:
				return
			case <-s.pushTicker.C:
				s.pushPeriodic(context.Background())
			}
		}
	}()
}

// pushPeriodic is the scheduled push driven by the metrics ticker.
// It includes the agent self-metrics in every batch so dashboards
// see continuous agent telemetry even when no probe data flowed in
// the current tick.
func (s *OTLPSyncStrategy) pushPeriodic(parent context.Context) {
	probesTotal, probesHealthy := agentstate.GetProbeCounts()
	agentRecords := agentmetrics.BuildAgentRecords(agentmetrics.AgentMetricsSnapshot{
		StartTime:          s.startTime,
		ProbesTotal:        probesTotal,
		ProbesHealthy:      probesHealthy,
		CollectErrorsTotal: agentstate.GetCollectErrorsTotal(),
		BuildVersion:       cliArgs.Version,
		BuildCommit:        cliArgs.CommitHash,
	})
	s.doPush(parent, agentRecords)
}

// pushDrain is the shutdown final push — emits only whatever probe
// data sits in the store, no agent records. Splitting from the
// periodic path keeps the shutdown sequence bounded: if the
// collector is unreachable when the agent is stopping, we don't
// generate fresh records that'll get stuck waiting for a dial that
// will never succeed.
func (s *OTLPSyncStrategy) pushDrain(parent context.Context) {
	s.doPush(parent, nil)
}

// doPush is the shared work: snapshot the store, resolve through
// otelmapper, push via the gRPC exporter. Errors are logged but
// never propagated — the next tick gets a fresh snapshot.
// Cumulative counter semantics mean the consumer sees no gap from
// a transient export failure (the next push carries the same or
// higher value).
//
// The push is wrapped in an OTel span so operators can correlate
// export latency / failure with the same observability backend that
// receives the metrics. The span is a no-op when traces are disabled
// (otel.Tracer returns the global noop tracer in that case).
func (s *OTLPSyncStrategy) doPush(parent context.Context, extraRecords []otelmapper.OtelRecord) {
	if s.exporters == nil || s.exporters.metric == nil {
		return
	}

	tracer := otel.Tracer(tracesScopeName)
	parent, span := tracer.Start(parent, "otlp.push.metrics") // Endpoint as attribute so split-backend configs (per-signal
	// override) show the actual target the span pushed to.
	// Set at start so it's present even if the push panics.

	span.SetAttributes(
		attribute.String("otlp.endpoint", s.cfg.Metrics.ResolveEndpoint(s.cfg.Endpoint)),
	)
	defer span.End()

	ctx, cancel := context.WithTimeout(parent, s.cfg.Timeout)
	defer cancel()

	now := time.Now()
	resolveOpts := otelmapper.ResolveOptions{IncludeProbeTags: true}

	// Snapshot store cardinality before the push so the gauge reflects
	// the size that drove this batch — useful when correlating export
	// duration spikes with cardinality growth.
	agentstate.RecordOTLPStoreSize(s.store.size())

	exportStart := time.Now()
	count, err := pushMetrics(
		ctx,
		s.store,
		s.registry,
		s.resource,
		cliArgs.Version,
		s.startTime,
		now,
		resolveOpts,
		s.globalTagKeys,
		extraRecords,
		func(ctx context.Context, rm *metricdata.ResourceMetrics) error {
			return s.exporters.metric.Export(ctx, rm)
		},
		s.warnMissingMappingOnce,
		s.cfg.MaxConcurrentExports,
	)
	exportDuration := time.Since(exportStart)
	span.SetAttributes(attribute.Int("otlp.records_count", count))
	if err != nil {
		// Span status is also exported via OTLP — redact the same way.
		redacted := redactSensitive(err.Error())
		span.RecordError(err)
		span.SetStatus(codes.Error, redacted)
		s.logger.Warn().Str("error", redacted).Dur("duration", exportDuration).Msg("OTLP metrics export failed")
		agentstate.IncrementOTLPExportErrors()
		return
	}
	span.SetStatus(codes.Ok, "")
	if count > 0 {
		s.logger.Debug().Int("records_pushed", count).Dur("duration", exportDuration).Msg("OTLP metrics exported")
		agentstate.IncrementOTLPMetricsPushed(count)
		agentstate.RecordOTLPExportDuration(exportDuration)
	}
}

// warnMissingMappingOnce logs the "metric has no OTel mapping" warning
// at most once per (probe_type, metric_name) for the strategy's
// lifetime. Avoids log spam when an operator has a misconfigured probe.
func (s *OTLPSyncStrategy) warnMissingMappingOnce(m otelmapper.CacheMetric, err error) {
	key := m.ProbeType + ":" + m.MetricName
	if _, seen := s.missingMappingWarned.LoadOrStore(key, struct{}{}); seen {
		return
	}
	s.logger.Warn().
		Err(err).
		Str("probe_name", m.ProbeName).
		Str("probe_type", m.ProbeType).
		Str("metric_name", m.MetricName).
		Msg("Metric has no OTel mapping — not exported via OTLP. Add an otel: block to the probe YAML or otel.skip: true to silence.")
}

// AddDataPoints stores the latest value for each series in the
// strategy-local LWW cache. The push goroutine reads from this cache
// every Metrics.Interval. Datapoints without probe identity are
// silently dropped (they cannot be routed through otelmapper).
func (s *OTLPSyncStrategy) AddDataPoints(data []datapoint.DataPoint) error {
	for _, dp := range data {
		s.store.upsert(dp)
	}
	return nil
}

// startEntityEmission wires the entity pump (consumer of the neutral
// entity-event channel) and the Detector (producer of the Lot 1 foundation
// events: host + service.instance + runs_on). Called from Start only when
// Entities.Enabled and the log pipeline exists.
//
// The entity's service.instance.id is the resource's service.instance.id so
// the entity identity and the OTLP resource agree on who the agent is.
func (s *OTLPSyncStrategy) startEntityEmission() {
	s.entityPump = newEntityPump(s.logs, s.cfg.Entities.BufferSize, s.logger)
	s.entityPump.start()

	serviceName := s.cfg.Resource.ServiceName
	if serviceName == "" {
		serviceName = "senhub-agent"
	}
	hostFn := func() (entity.HostIdentity, error) {
		hi, err := common.GetHostIdentity()
		if err != nil {
			return entity.HostIdentity{}, err
		}
		return entity.HostIdentity{ID: hi.ID, Name: hi.Name, OSType: hi.OSType}, nil
	}
	agentFn := func() entity.AgentIdentity {
		return entity.AgentIdentity{
			InstanceID:     s.cfg.Resource.ServiceInstance,
			ServiceName:    serviceName,
			ServiceVersion: cliArgs.Version,
		}
	}

	// Host-side topology source (entity Lot 4): emits the host's upstream
	// gateways as network.device + routes_via, reusing the entity rail. Only
	// active while entity emission runs.
	entity.RegisterSource(hostnet.New(func() string {
		hi, err := common.GetHostIdentity()
		if err != nil {
			return ""
		}
		return hi.ID
	}))

	det := entity.NewDetector(hostFn, agentFn, s.cfg.Entities.Interval)
	det.OnOrphanRelations(func(orphans []entity.Relation) {
		for _, r := range orphans {
			s.logger.Warn().
				Str("relation", r.Type).
				Str("from_type", r.FromType).
				Str("to_type", r.ToType).
				Msg("entity relation has no source entity this cycle; dropped from the wire")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	s.entityDetectorCancel = cancel
	s.entityDetectorWG.Add(1)
	go func() {
		defer s.entityDetectorWG.Done()
		det.Run(ctx)
	}()
}

// Shutdown stops the strategy: signals the push goroutine, waits for it
// to drain, performs a final push (so the last interval's data isn't
// lost), then closes the gRPC exporters. Idempotent: once shut down,
// subsequent calls are no-ops. Start cannot bring the strategy back up.
func (s *OTLPSyncStrategy) Shutdown(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if s.shutdown {
		return nil
	}
	s.shutdown = true
	s.started = false

	// Stop the periodic ticker and wait for the goroutine to exit. We
	// do this BEFORE the final push to avoid a race where both the
	// timer-driven push and the shutdown-driven push overlap on the
	// same exporter.
	if s.pushTicker != nil {
		s.pushTicker.Stop()
		close(s.pushDone)
		s.pushWG.Wait()
	}

	// Stop the memory limiter poller. Safe even if start() was never
	// called (stop becomes a no-op via the once-guard).
	if s.memLimiter != nil {
		s.memLimiter.stop()
	}

	// Stop the checkpointer — performs a final synchronous save so
	// graceful shutdown preserves the latest state. Same once-guard
	// pattern as memLimiter.
	if s.chkpt != nil {
		s.chkpt.stop()
	}

	// Stop entity emission: cancel the Detector (producer) and wait, then
	// stop the pump (consumer). Producer-before-consumer so no event is
	// published after the channel subscription is gone.
	if s.entityDetectorCancel != nil {
		s.entityDetectorCancel()
		s.entityDetectorWG.Wait()
	}
	if s.entityPump != nil {
		s.entityPump.stop(ctx)
	}

	// Stop the logs pump and unsubscribe from agentstate. The
	// LoggerProvider's Shutdown (called below via s.logs.shutdown)
	// drains the BatchProcessor, so any records still queued in the
	// SDK will be flushed before the gRPC connection closes.
	if s.logsPump != nil {
		s.logsPump.stop(ctx)
	}

	if s.exporters == nil {
		return nil
	}

	// If the caller passed a context without a deadline, apply a
	// reasonable default so a stuck collector doesn't hang shutdown
	// forever.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	// Drain — final push of whatever sits in the store. Failures here
	// are best-effort: we still want to close the exporters even if
	// the last push fails (collector down, etc.).
	if s.exporters.metric != nil && s.cfg.Metrics.Enabled {
		s.pushDrain(ctx)
	}

	// Drain the log pipeline before shutting down the gRPC exporter.
	// The provider.Shutdown call flushes any queued records via the
	// BatchProcessor, then shuts down the underlying exporter — so
	// the subsequent exporters.shutdown will see an already-closed
	// log exporter (its second Shutdown is a no-op per SDK semantics).
	if s.logs != nil {
		if err := s.logs.shutdown(ctx); err != nil {
			s.logger.Warn().Err(err).Msg("OTLP logs pipeline shutdown failed")
		}
	}

	// Same drain pattern for traces: provider.Shutdown flushes the
	// BatchSpanProcessor before closing the underlying exporter.
	if s.traces != nil {
		if err := s.traces.shutdown(ctx); err != nil {
			s.logger.Warn().Err(err).Msg("OTLP traces pipeline shutdown failed")
		}
	}

	if err := s.exporters.shutdown(ctx); err != nil {
		s.logger.Warn().Err(err).Msg("OTLP strategy shutdown encountered errors")
		return err
	}

	s.logger.Info().Int("series_in_store", s.store.size()).Msg("OTLP strategy shut down cleanly")
	return nil
}

// Config returns the parsed configuration. Read-only access for
// upcoming Phase 3 integration code that needs to know intervals,
// resource attrs, etc.
func (s *OTLPSyncStrategy) Config() Config {
	return s.cfg
}

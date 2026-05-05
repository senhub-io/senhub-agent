package otlp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// strategyName is the canonical identifier used in the data_store
// `endpoints:` configuration. Matches the operator-facing storage name.
const strategyName = "otlp"

// OTLPSyncStrategy implements data_store.SyncStrategy by exporting
// metrics and logs over OTLP/gRPC.
//
// Phase 1: lifecycle skeleton — config parsing, exporter wiring,
// graceful shutdown. AddDataPoints currently just counts datapoints
// for diagnostic purposes; metrics export from the cache and logs
// export from the log channel land in Phase 2 and Phase 3.
type OTLPSyncStrategy struct {
	agentConfig configuration.AgentConfiguration
	rawParams   map[string]interface{}
	cfg         Config
	logger      *logger.ModuleLogger

	exporters *exporters

	startMu  sync.Mutex
	started  bool
	shutdown bool

	// dataPointsSeen counts datapoints AddDataPoints has handled. Phase
	// 1 doesn't ship them yet; Phase 2 wires the cache-based push so
	// this counter stays only as a diagnostic during the transition.
	dataPointsSeen uint64
	dpMu           sync.Mutex
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

	return &OTLPSyncStrategy{
		agentConfig: agentConfig,
		rawParams:   params,
		cfg:         cfg,
		logger:      moduleLogger,
	}
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

// Start brings the strategy online: builds the OTel SDK exporters
// (which do not yet open a gRPC connection — that's lazy on first
// export). Subsequent calls are no-ops.
//
// Phase 1: the goroutines that drive periodic metric pushes (Phase 2)
// and consume the log channel (Phase 3) are NOT started here yet. We'll
// add them at the corresponding phase boundaries.
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

	exp, err := buildExporters(ctx, s.cfg)
	if err != nil {
		return fmt.Errorf("build exporters: %w", err)
	}
	s.exporters = exp

	s.logger.Info().
		Str("endpoint", s.cfg.Endpoint).
		Bool("tls_enabled", s.cfg.TLS.Enabled).
		Bool("metrics_enabled", s.cfg.Metrics.Enabled).
		Bool("logs_enabled", s.cfg.Logs.Enabled).
		Str("compression", s.cfg.Compression).
		Dur("timeout", s.cfg.Timeout).
		Msg("OTLP strategy started — Phase 1 (skeleton; metrics/logs export not yet wired)")

	s.started = true
	return nil
}

// AddDataPoints accepts datapoints from the data_store router. Phase 1
// just counts them — they sit in the existing MetricCache via the http
// strategy already, and Phase 2 will read from the cache to push.
//
// This is a deliberately quiet no-op rather than an error: configuring
// `endpoints: [otlp, ...]` on probes during Phase 1 should not break
// the probe; it should just mean "metrics aren't pushed yet, but they
// will be once Phase 2 ships".
func (s *OTLPSyncStrategy) AddDataPoints(data []datapoint.DataPoint) error {
	s.dpMu.Lock()
	s.dataPointsSeen += uint64(len(data))
	s.dpMu.Unlock()
	return nil
}

// Shutdown stops the strategy and releases the gRPC exporters. Idempotent:
// once shut down, subsequent calls are no-ops. Start cannot bring the
// strategy back up — the caller must build a fresh instance.
func (s *OTLPSyncStrategy) Shutdown(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if s.shutdown {
		return nil
	}
	s.shutdown = true
	s.started = false

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

	if err := s.exporters.shutdown(ctx); err != nil {
		s.logger.Warn().Err(err).Msg("OTLP strategy shutdown encountered errors")
		return err
	}

	s.logger.Info().Uint64("datapoints_seen", s.dataPointsSeen).Msg("OTLP strategy shut down cleanly")
	return nil
}

// Config returns the parsed configuration. Read-only access for
// upcoming Phase 2/3 integration code that needs to know intervals,
// resource attrs, etc.
func (s *OTLPSyncStrategy) Config() Config {
	return s.cfg
}

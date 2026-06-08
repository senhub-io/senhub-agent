package otlp

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// Endpoint failover (issue #217, resilience layer 2).
//
// A single public intake is a SPOF: today the agent has one endpoint and
// the SDK retry only re-hits that same endpoint. With fallback_endpoints
// configured, the agent builds one exporter per endpoint and routes
// through a failover decorator that prefers the primary, switches to a
// standby on a failed export, and returns to the primary automatically
// once it recovers. A per-endpoint cooldown avoids paying the primary's
// (failed) retry latency on every cycle while it is down.
//
// This composes under the logs dead-letter queue: persistentLogExporter
// wraps the failover exporter, so a record is only persisted when EVERY
// endpoint is down — failover handles "primary down, standby up" without
// touching disk.

const defaultFailoverCooldown = 30 * time.Second

// endpointState tracks one candidate endpoint's health for the failover
// core. downUntil (unix-nanos) is a cooldown deadline: while in the
// future, the endpoint is skipped in the preferred-order pass.
type endpointState struct {
	endpoint  string
	downUntil atomic.Int64
}

// failoverCore holds the shared selection logic for both the metric and
// log failover decorators. It is concurrency-safe.
type failoverCore struct {
	states   []*endpointState
	cooldown time.Duration
	active   atomic.Int32
	logger   *logger.ModuleLogger
}

func newFailoverCore(endpoints []string, cooldown time.Duration, log *logger.ModuleLogger) *failoverCore {
	if cooldown <= 0 {
		cooldown = defaultFailoverCooldown
	}
	states := make([]*endpointState, len(endpoints))
	for i, ep := range endpoints {
		states[i] = &endpointState{endpoint: ep}
	}
	return &failoverCore{states: states, cooldown: cooldown, logger: log}
}

// do runs attempt against endpoints in preferred order (index 0 first),
// skipping any in cooldown. The first success wins; a failure puts that
// endpoint in cooldown and moves on. If every endpoint is in cooldown,
// a second pass tries them all anyway — a possibly-recovered endpoint
// beats dropping the batch. Returns the last error when all fail.
func (c *failoverCore) do(attempt func(idx int) error) error {
	nowNano := time.Now().UnixNano()
	var lastErr error
	anyTried := false

	for idx := range c.states {
		if c.states[idx].downUntil.Load() > nowNano {
			continue // in cooldown
		}
		anyTried = true
		if err := attempt(idx); err == nil {
			c.markActive(idx)
			return nil
		} else {
			c.states[idx].downUntil.Store(time.Now().Add(c.cooldown).UnixNano())
			lastErr = err
		}
	}

	if !anyTried {
		// Everything is cooling down; try them all in preference order so
		// a recovered endpoint is picked up immediately.
		for idx := range c.states {
			if err := attempt(idx); err == nil {
				c.states[idx].downUntil.Store(0)
				c.markActive(idx)
				return nil
			} else {
				lastErr = err
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("otlp failover: no endpoints configured")
	}
	return lastErr
}

// markActive records the endpoint now serving; logs + counts a switch
// when it changed.
func (c *failoverCore) markActive(idx int) {
	prev := c.active.Swap(int32(idx))
	agentstate.RecordOTLPActiveEndpointIndex(idx)
	if int(prev) != idx {
		agentstate.IncrementOTLPEndpointSwitches()
		if c.logger != nil {
			c.logger.Warn().
				Int("from_index", int(prev)).
				Int("to_index", idx).
				Str("endpoint", c.states[idx].endpoint).
				Msg("OTLP endpoint failover: switched active endpoint")
		}
	}
}

// failoverMetricExporter is the sdkmetric.Exporter failover decorator.
type failoverMetricExporter struct {
	core *failoverCore
	exps []sdkmetric.Exporter
}

func (f *failoverMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return f.exps[0].Temporality(k)
}
func (f *failoverMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return f.exps[0].Aggregation(k)
}
func (f *failoverMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	return f.core.do(func(i int) error { return f.exps[i].Export(ctx, rm) })
}
func (f *failoverMetricExporter) ForceFlush(ctx context.Context) error {
	var errs []error
	for _, e := range f.exps {
		if err := e.ForceFlush(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (f *failoverMetricExporter) Shutdown(ctx context.Context) error {
	var errs []error
	for _, e := range f.exps {
		if err := e.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// failoverLogExporter is the sdklog.Exporter failover decorator.
type failoverLogExporter struct {
	core *failoverCore
	exps []sdklog.Exporter
}

func (f *failoverLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	return f.core.do(func(i int) error { return f.exps[i].Export(ctx, records) })
}
func (f *failoverLogExporter) ForceFlush(ctx context.Context) error {
	var errs []error
	for _, e := range f.exps {
		if err := e.ForceFlush(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (f *failoverLogExporter) Shutdown(ctx context.Context) error {
	var errs []error
	for _, e := range f.exps {
		if err := e.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// allEndpoints returns the primary followed by the configured fallbacks.
func allEndpoints(cfg Config) []string {
	eps := make([]string, 0, 1+len(cfg.FallbackEndpoints))
	eps = append(eps, cfg.Endpoint)
	eps = append(eps, cfg.FallbackEndpoints...)
	return eps
}

// buildFailoverMetricExporter builds one metric exporter per endpoint and
// wraps them in the failover decorator. On a build error it shuts down the
// exporters already created so no half-built bundle leaks.
func buildFailoverMetricExporter(ctx context.Context, cfg Config, log *logger.ModuleLogger) (sdkmetric.Exporter, error) {
	eps := allEndpoints(cfg)
	exps := make([]sdkmetric.Exporter, 0, len(eps))
	for _, ep := range eps {
		c := cfg
		c.Endpoint = ep
		e, err := buildMetricExporter(ctx, c)
		if err != nil {
			for _, built := range exps {
				_ = built.Shutdown(ctx)
			}
			return nil, err
		}
		exps = append(exps, e)
	}
	return &failoverMetricExporter{core: newFailoverCore(eps, defaultFailoverCooldown, log), exps: exps}, nil
}

// buildFailoverLogExporter builds one log exporter per endpoint and wraps
// them in the failover decorator.
func buildFailoverLogExporter(ctx context.Context, cfg Config, log *logger.ModuleLogger) (sdklog.Exporter, error) {
	eps := allEndpoints(cfg)
	exps := make([]sdklog.Exporter, 0, len(eps))
	for _, ep := range eps {
		c := cfg
		c.Endpoint = ep
		e, err := buildLogExporter(ctx, c)
		if err != nil {
			for _, built := range exps {
				_ = built.Shutdown(ctx)
			}
			return nil, err
		}
		exps = append(exps, e)
	}
	return &failoverLogExporter{core: newFailoverCore(eps, defaultFailoverCooldown, log), exps: exps}, nil
}

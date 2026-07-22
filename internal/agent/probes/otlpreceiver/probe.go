// Package otlpreceiver implements an event-driven probe that runs an
// embedded OTLP receiver (gRPC or HTTP) and ingests incoming telemetry as
// if it had been collected by an internal probe. The agent thus acts as a
// small edge collector, aggregating OTLP streams from other instrumented
// devices/applications and routing them to the configured sinks.
//
// Which signals the listener accepts is config-driven (`signals:`, metrics
// only by default). Metrics become datapoints on the probe callback; logs
// are published on the agent log channel and relayed by the OTLP export
// strategy; trace spans are published verbatim (raw OTLP proto, no
// internal model) on the agent span channel and relayed the same way.
//
// The probe mirrors the event-driven contract used by the syslog probe:
// it implements ProbeWithCallback (SetCallback), opens its listener in
// OnStart, pushes decoded datapoints through the callback, and tears the
// listener down in OnShutdown. Collect() is a no-op because the data is
// pushed, not polled.
package otlpreceiver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/utils/netbind"
)

const (
	probeType = "otlp_receiver"
	// noSinkWarnInterval throttles the "ingested logs/spans have nowhere
	// to go" warnings so a sender pushing at line rate cannot flood the
	// agent log.
	noSinkWarnInterval = 5 * time.Minute
)

// OTLPReceiverProbe runs an embedded OTLP receiver: ingested metrics go to
// the data store via the probe callback, ingested logs onto the agent log
// channel and ingested spans onto the agent span channel, both relayed by
// a capable strategy (the OTLP export pipeline).
type OTLPReceiverProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       receiverConfig
	guard        *ingressGuard
	moduleLogger *logger.ModuleLogger

	mu                 sync.Mutex
	grpcServer         *grpc.Server
	httpServer         interface{ shutdown(context.Context) error }
	listener           net.Listener
	callback           func([]data_store.DataPoint) error
	lastNoSinkWarn     time.Time
	lastNoSpanSinkWarn time.Time
}

// NewOTLPReceiverProbe constructs the probe from its raw config map.
func NewOTLPReceiverProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.otlp_receiver")

	cfg, err := parseReceiverConfig(config)
	if err != nil {
		return nil, fmt.Errorf("parsing otlp_receiver config: %w", err)
	}

	moduleLogger.Debug().
		Str("protocol", cfg.Protocol).
		Str("address", cfg.Address).
		Msg("Creating new OTLP receiver probe")

	probe := &OTLPReceiverProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		config:       cfg,
		guard:        newIngressGuard(cfg),
		moduleLogger: moduleLogger,
	}
	probe.SetProbeType(probeType)
	return probe, nil
}

// SetCallback registers the data store callback. Required by the
// event-driven ProbeWithCallback contract.
func (p *OTLPReceiverProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callback = callback
}

func (p *OTLPReceiverProbe) ShouldStart() bool { return true }

// GetInterval is unused for an event-driven probe but the scheduler
// still reads it; a non-zero value keeps the periodic_scheduler happy.
func (p *OTLPReceiverProbe) GetInterval() time.Duration { return 30 * time.Second }

// Collect is a no-op: data arrives via the receiver and the callback.
func (p *OTLPReceiverProbe) Collect() ([]data_store.DataPoint, error) { return nil, nil }

// OnStart opens the configured listener and starts serving.
func (p *OTLPReceiverProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("protocol", p.config.Protocol).
		Str("address", p.config.Address).
		Strs("signals", p.config.Signals.names()).
		Msg("Starting OTLP receiver")

	if netbind.IsWildcard(p.config.Address) {
		p.moduleLogger.Warn().
			Str("address", p.config.Address).
			Msg("OTLP receiver bound to ALL interfaces without authentication — restrict `address` or firewall the port")
	}

	switch p.config.Protocol {
	case protocolGRPC:
		return p.startGRPC(quitChannel)
	case protocolHTTP:
		return p.startHTTP(quitChannel)
	default:
		return fmt.Errorf("unsupported protocol %q", p.config.Protocol)
	}
}

// OnShutdown gracefully stops whichever server is running.
func (p *OTLPReceiverProbe) OnShutdown(ctx context.Context) error {
	p.mu.Lock()
	grpcServer := p.grpcServer
	httpServer := p.httpServer
	p.mu.Unlock()

	if grpcServer != nil {
		p.moduleLogger.Info().Msg("Stopping OTLP gRPC receiver")
		grpcServer.GracefulStop()
	}
	if httpServer != nil {
		p.moduleLogger.Info().Msg("Stopping OTLP HTTP receiver")
		return httpServer.shutdown(ctx)
	}
	return nil
}

// logRejection traces a guard rejection. Auth and allow-list failures
// are operator-actionable and logged at Warn; rate-limit rejections
// can fire at line rate during a burst and stay at Debug.
func (p *OTLPReceiverProbe) logRejection(remote string, err error) {
	evt := p.moduleLogger.Warn()
	if errors.Is(err, errRateLimited) {
		evt = p.moduleLogger.Debug()
	}
	evt.Str("remote", remote).Err(err).Msg("OTLP ingest request rejected")
}

// ingest decodes a received metrics payload and forwards the resulting
// datapoints through the callback. The dropped count carries the number
// of metrics with an unrecognized or unset data type the flattener could
// not map, so the gRPC/HTTP layer can build a partial-success response.
func (p *OTLPReceiverProbe) ingest(points []data_store.DataPoint, dropped int) error {
	p.mu.Lock()
	callback := p.callback
	p.mu.Unlock()

	if dropped > 0 {
		p.moduleLogger.Debug().
			Int("dropped", dropped).
			Msg("Dropped OTLP metrics with an unrecognized or unset data type")
	}

	if len(points) == 0 {
		return nil
	}
	if callback == nil {
		p.moduleLogger.Warn().Msg("OTLP receiver callback not set; dropping ingested datapoints")
		return nil
	}

	enriched := p.EnrichDataPointsWithProbeName(points, p.GetName())
	if err := callback(enriched); err != nil {
		p.moduleLogger.Error().Err(err).Msg("Failed to forward ingested OTLP datapoints to data store")
		return err
	}
	p.moduleLogger.Debug().Int("datapoints", len(points)).Msg("Ingested OTLP datapoints")
	return nil
}

// ingestLogs publishes received OTLP log records on the agent log channel,
// from which a log-capable strategy (the OTLP export pipeline) drains them.
// The receiver only relays: the pull sinks are metrics-only, so with no OTLP
// export strategy subscribed the records have nowhere to go and the operator
// gets a throttled warning rather than a silent void.
func (p *OTLPReceiverProbe) ingestLogs(records []agentstate.LogRecord) {
	if len(records) == 0 {
		return
	}
	if agentstate.LogSubscriberCount() == 0 {
		p.warnNoLogSink(len(records))
		return
	}
	for _, rec := range records {
		agentstate.PublishLog(rec)
	}
	p.moduleLogger.Debug().Int("records", len(records)).Msg("Ingested OTLP log records")
}

// warnNoLogSink warns at most once per noSinkWarnInterval that ingested logs
// are being discarded because no log-capable strategy is configured.
func (p *OTLPReceiverProbe) warnNoLogSink(dropped int) {
	p.mu.Lock()
	now := time.Now()
	warn := now.Sub(p.lastNoSinkWarn) >= noSinkWarnInterval
	if warn {
		p.lastNoSinkWarn = now
	}
	p.mu.Unlock()

	if warn {
		p.moduleLogger.Warn().
			Int("dropped", dropped).
			Msg("Ingested OTLP logs discarded: no OTLP export strategy is configured to relay them")
	}
}

// ingestSpans publishes received OTLP spans on the agent span channel as
// raw ResourceSpans, from which the OTLP export strategy relays them
// verbatim. Spans have no internal scalar model, so they bypass the
// DataPoint path entirely. Same no-sink contract as logs: with no
// subscriber (no OTLP export strategy with signals.traces enabled) the
// spans have nowhere to go and the operator gets a throttled warning.
func (p *OTLPReceiverProbe) ingestSpans(rs []*tracepb.ResourceSpans) {
	if len(rs) == 0 {
		return
	}
	if agentstate.SpanSubscriberCount() == 0 {
		p.warnNoSpanSink(len(rs))
		return
	}
	agentstate.PublishSpans(rs)
	p.moduleLogger.Debug().Int("resource_spans", len(rs)).Msg("Ingested OTLP spans")
}

// warnNoSpanSink warns at most once per noSinkWarnInterval that ingested
// spans are being discarded because no trace-capable strategy is
// configured.
func (p *OTLPReceiverProbe) warnNoSpanSink(dropped int) {
	p.mu.Lock()
	now := time.Now()
	warn := now.Sub(p.lastNoSpanSinkWarn) >= noSinkWarnInterval
	if warn {
		p.lastNoSpanSinkWarn = now
	}
	p.mu.Unlock()

	if warn {
		p.moduleLogger.Warn().
			Int("dropped", dropped).
			Msg("Ingested OTLP spans discarded: no OTLP export strategy has signals.traces enabled to relay them")
	}
}

// replacePort swaps the port portion of a host:port address. If the
// address has no parseable port the original host is kept and the new
// port appended.
func replacePort(addr string, port int) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

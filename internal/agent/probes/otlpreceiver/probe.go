// Package otlpreceiver implements an event-driven probe that runs an
// embedded OTLP metrics receiver (gRPC or HTTP) and ingests incoming
// datapoints as if they had been collected by an internal probe. The
// agent thus acts as a small edge collector, aggregating OTLP streams
// from other instrumented devices/applications and routing them to the
// configured sinks.
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

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/utils/netbind"
)

const probeType = "otlp_receiver"

// OTLPReceiverProbe runs an embedded OTLP metrics receiver and forwards
// every ingested datapoint to the data store via the probe callback.
type OTLPReceiverProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       receiverConfig
	guard        *ingressGuard
	moduleLogger *logger.ModuleLogger

	mu         sync.Mutex
	grpcServer *grpc.Server
	httpServer interface{ shutdown(context.Context) error }
	listener   net.Listener
	callback   func([]data_store.DataPoint) error
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
// of non-scalar datapoints (histogram/summary) the flattener could not
// map, so the gRPC/HTTP layer can build a partial-success response.
func (p *OTLPReceiverProbe) ingest(points []data_store.DataPoint, dropped int) error {
	p.mu.Lock()
	callback := p.callback
	p.mu.Unlock()

	if dropped > 0 {
		p.moduleLogger.Debug().
			Int("dropped", dropped).
			Msg("Dropped non-scalar OTLP datapoints (histogram/summary not mapped to a scalar value)")
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

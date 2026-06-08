package otlp

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// logsScopeName identifies the Logger that produced these records,
// per OTel spec — paired with a version string (build version, or
// "dev" if unknown).
const logsScopeName = "senhub-agent/otlp-logs"

// entitiesScopeName identifies the Logger that emits entity/relation events.
// Its instrumentation scope carries otel.entity.entity_event=true — the
// fast-path filter convention for entity-aware consumers (collector-contrib).
// Toise ignores the flag, but setting it preserves interop.
const entitiesScopeName = "senhub-agent/otlp-entities"

const scopeAttrEntityEvent = "otel.entity.entity_event"

// logsPipeline bundles the SDK objects that drive log export. The
// LoggerProvider owns the BatchProcessor that owns the OTLP exporter
// — graceful shutdown happens by calling provider.Shutdown(ctx) which
// drains the batch processor's queue first.
//
// Constructed by buildLogsPipeline when Logs.Enabled; otherwise nil.
type logsPipeline struct {
	provider *sdklog.LoggerProvider
	logger   log.Logger
	// entityLogger emits entity/relation events. Separate Logger (distinct
	// instrumentation scope + the otel.entity.entity_event scope attribute)
	// off the same provider/exporter, so entity events ride the log signal
	// without mixing scopes with ordinary logs.
	entityLogger log.Logger
}

// buildLogsPipeline wires the SDK BatchProcessor onto the provided
// gRPC log exporter and returns the resulting LoggerProvider + Logger.
//
// The SDK BatchProcessor is the canonical OTel pattern for log
// batching:
//   - WithMaxQueueSize:        max records buffered before drop
//   - WithExportInterval:      flush cadence (BatchTimeout)
//   - WithExportMaxBatchSize:  max records per flush (BatchSize)
//
// Configuring our own custom batcher would duplicate the SDK's logic
// (and its bug fixes); building on top means our log path tracks
// upstream improvements automatically.
//
// The Logger's instrumentation scope is the version-aware identifier
// every emitted record carries. Receivers see scope.name +
// scope.version per the OTel logs data model.
func buildLogsPipeline(
	exporter sdklog.Exporter,
	res *resource.Resource,
	cfg LogsSignal,
	scopeVersion string,
) *logsPipeline {
	if scopeVersion == "" {
		scopeVersion = "dev"
	}

	processor := sdklog.NewBatchProcessor(
		exporter,
		sdklog.WithMaxQueueSize(cfg.BufferSize),
		sdklog.WithExportInterval(cfg.BatchTimeout),
		sdklog.WithExportMaxBatchSize(cfg.BatchSize),
	)

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(processor),
	)

	return &logsPipeline{
		provider: provider,
		logger:   provider.Logger(logsScopeName, log.WithInstrumentationVersion(scopeVersion)),
		entityLogger: provider.Logger(
			entitiesScopeName,
			log.WithInstrumentationVersion(scopeVersion),
			log.WithInstrumentationAttributes(attribute.Bool(scopeAttrEntityEvent, true)),
		),
	}
}

// emitEntityRecord hands a pre-encoded entity/relation Record to the entity
// Logger. The record is built by the entity pump via buildEntityRecord; this
// just attaches scope + resource and queues it on the same BatchProcessor as
// logs.
func (p *logsPipeline) emitEntityRecord(ctx context.Context, rec log.Record) {
	if p == nil || p.entityLogger == nil {
		return
	}
	p.entityLogger.Emit(ctx, rec)
	agentstate.IncrementOTLPLogsPushed()
}

// emit converts an agent-internal LogRecord into the OTel API Record
// and hands it off to the SDK Logger. The Logger does the rest:
// timestamps, scope, resource attached, then the BatchProcessor
// queues for export.
//
// Pulled out as a method (vs inline at the call site) so producers
// can be unit-tested against a recording fake Logger that just stores
// the api records — see logs_pump_test.go.
func (p *logsPipeline) emit(ctx context.Context, rec agentstate.LogRecord) {
	if p == nil || p.logger == nil {
		return
	}
	var apiRec log.Record
	apiRec.SetTimestamp(rec.Timestamp)
	apiRec.SetObservedTimestamp(rec.Timestamp)
	apiRec.SetSeverity(log.Severity(rec.Severity))
	if rec.SeverityText != "" {
		apiRec.SetSeverityText(rec.SeverityText)
	}
	apiRec.SetBody(log.StringValue(rec.Body))

	attrs := make([]log.KeyValue, 0, len(rec.Attributes)+2)
	if rec.ProducerProbeName != "" {
		attrs = append(attrs, log.String("senhub.probe.name", rec.ProducerProbeName))
	}
	if rec.ProducerProbeType != "" {
		attrs = append(attrs, log.String("senhub.probe.type", rec.ProducerProbeType))
	}
	for k, v := range rec.Attributes {
		attrs = append(attrs, log.String(k, v))
	}
	if len(attrs) > 0 {
		apiRec.AddAttributes(attrs...)
	}

	p.logger.Emit(ctx, apiRec)
	agentstate.IncrementOTLPLogsPushed()
}

// replayEventLog rebuilds an API log.Record from a persisted event log
// and re-emits it through the ordinary-logs Logger (logsScopeName), which
// re-attaches scope + resource — neither is settable on a raw record, so
// replay must go through the pipeline rather than the exporter (#217).
func (p *logsPipeline) replayEventLog(ctx context.Context, pr persistedLogRecord) {
	if p == nil || p.logger == nil {
		return
	}
	var apiRec log.Record
	apiRec.SetTimestamp(time.Unix(0, pr.TimestampUnixNano))
	if pr.ObservedTimestampUnixNano != 0 {
		apiRec.SetObservedTimestamp(time.Unix(0, pr.ObservedTimestampUnixNano))
	} else {
		apiRec.SetObservedTimestamp(time.Unix(0, pr.TimestampUnixNano))
	}
	apiRec.SetSeverity(log.Severity(pr.SeverityNumber))
	if pr.SeverityText != "" {
		apiRec.SetSeverityText(pr.SeverityText)
	}
	apiRec.SetBody(log.StringValue(pr.Body))
	if len(pr.Attributes) > 0 {
		attrs := make([]log.KeyValue, 0, len(pr.Attributes))
		for k, v := range pr.Attributes {
			attrs = append(attrs, log.String(k, v))
		}
		apiRec.AddAttributes(attrs...)
	}
	p.logger.Emit(ctx, apiRec)
	agentstate.IncrementOTLPLogsPushed()
}

// shutdown drains the BatchProcessor and shuts the provider down,
// honoring the caller's context deadline.
func (p *logsPipeline) shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

// logsPump consumes the agentstate log channel and forwards each
// record to the OTel SDK Logger. One pump per strategy instance.
//
// Lifecycle:
//   - run: subscribe → loop until done is closed → unsubscribe
//   - the goroutine returns either when the agentstate channel closes
//     (UnsubscribeLogs called) OR when its context is cancelled
//
// Backpressure: PublishLog uses drop-oldest into the subscription
// buffer; the SDK BatchProcessor's WithMaxQueueSize bounds memory
// downstream. The pump itself never blocks except in `<-channel`
// receives (cooperatively interruptible via cancel).
type logsPump struct {
	pipeline *logsPipeline
	bufSize  int

	mu         sync.Mutex
	subscribed <-chan agentstate.LogRecord
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newLogsPump(p *logsPipeline, bufSize int) *logsPump {
	if bufSize <= 0 {
		bufSize = 1024
	}
	return &logsPump{pipeline: p, bufSize: bufSize}
}

// start subscribes to the agentstate log channel and launches the
// drain goroutine. Idempotent — calling twice is a no-op (used to
// keep Strategy.Start straightforward; second Start should not
// re-subscribe).
func (p *logsPump) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.subscribed != nil {
		return
	}
	ch := agentstate.SubscribeLogs(p.bufSize)
	p.subscribed = ch
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.drain(ctx, ch)
}

// drain runs until the context is cancelled OR the agentstate channel
// closes. Records arriving after cancel are honored on a best-effort
// basis up to the deadline.
func (p *logsPump) drain(ctx context.Context, ch <-chan agentstate.LogRecord) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case rec, ok := <-ch:
			if !ok {
				return
			}
			p.pipeline.emit(ctx, rec)
		}
	}
}

// stop cancels the pump goroutine and unsubscribes. Honors deadline:
// if the goroutine doesn't exit within the deadline, returns without
// waiting (the goroutine will exit on its own when the channel closes
// from UnsubscribeLogs). Idempotent.
func (p *logsPump) stop(ctx context.Context) {
	p.mu.Lock()
	ch := p.subscribed
	cancel := p.cancel
	p.subscribed = nil
	p.cancel = nil
	p.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	if ch != nil {
		agentstate.UnsubscribeLogs(ch)
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// Best-effort; goroutine will exit when channel closes.
	case <-time.After(5 * time.Second):
	}
}

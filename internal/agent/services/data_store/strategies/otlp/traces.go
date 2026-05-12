package otlp

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// tracesScopeName identifies the Tracer that produced spans emitted
// by the strategy itself (lifecycle / push markers). Application code
// that wants its own scope should call otel.Tracer with its own name
// after the global provider is installed.
const tracesScopeName = "senhub-agent/otlp-traces"

// tracesPipeline bundles the SDK objects driving span export. The
// TracerProvider owns the BatchSpanProcessor that owns the OTLP
// exporter. Shutdown drains the batcher's queue before closing the
// underlying gRPC connection.
//
// Constructed by buildTracesPipeline when Traces.Enabled; otherwise
// nil. The pipeline registers itself as the OTel global TracerProvider
// so any code (including third-party libraries) that calls
// otel.GetTracerProvider() picks it up automatically.
type tracesPipeline struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
}

// buildTracesPipeline wires the SDK BatchSpanProcessor onto the
// provided gRPC trace exporter and returns the resulting
// TracerProvider + Tracer. The provider is also installed as the OTel
// global so any code that resolves a tracer via otel.Tracer()
// reaches this exporter.
//
// SDK options:
//   - WithBatcher: BatchSpanProcessor with cfg.Traces batching params
//   - WithResource: same Resource as metrics/logs (service.name etc.)
//   - WithSampler: parent-based(traceID-ratio) — production-friendly
func buildTracesPipeline(
	exporter *otlptrace.Exporter,
	res *resource.Resource,
	cfg TracesSignal,
	scopeVersion string,
) *tracesPipeline {
	if scopeVersion == "" {
		scopeVersion = "dev"
	}

	bsp := sdktrace.NewBatchSpanProcessor(
		exporter,
		sdktrace.WithMaxQueueSize(cfg.BufferSize),
		sdktrace.WithBatchTimeout(cfg.BatchTimeout),
		sdktrace.WithMaxExportBatchSize(cfg.BatchSize),
	)

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(provider)

	return &tracesPipeline{
		provider: provider,
		tracer:   provider.Tracer(tracesScopeName, trace.WithInstrumentationVersion(scopeVersion)),
	}
}

// shutdown drains the BatchSpanProcessor and shuts the provider down,
// honoring the caller's context deadline. Safe to call on a nil
// pipeline.
func (p *tracesPipeline) shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

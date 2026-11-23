package otlp

import (
	"context"
	"errors"
	"fmt"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// sdkOp is one SDK exporter call seen through a wireSlot: the transport hook
// fills the slot with the request the exporter built.
type sdkOp struct {
	n    int
	slot *wireSlot
	call func(ctx context.Context) error
}

func newSDKOp(units int, call func(ctx context.Context) error) *sdkOp {
	return &sdkOp{n: units, slot: &wireSlot{}, call: call}
}

func (o *sdkOp) units() int { return o.n }

func (o *sdkOp) send(ctx context.Context) error {
	return o.call(withWireSlot(ctx, o.slot))
}

func (o *sdkOp) payload(ctx context.Context, attempted bool) ([]byte, error) {
	if !attempted {
		// The batch goes to disk without a network attempt: the exporter
		// still encodes it, and the hook keeps it instead of sending it.
		o.slot.dry = true
		err := o.call(withWireSlot(context.WithoutCancel(ctx), o.slot))
		if !o.slot.captured() {
			return nil, fmt.Errorf("encoding the request: %w", err)
		}
	}
	return o.slot.request()
}

// persistentMetricExporter decorates the SDK metric exporter: a push the
// backend did not take is kept on disk as the request the exporter built, and
// replayed in order.
type persistentMetricExporter struct {
	sdkmetric.Exporter
	ch *wireChannel
}

func newPersistentMetricExporter(wrapped sdkmetric.Exporter, ch *wireChannel) *persistentMetricExporter {
	e := &persistentMetricExporter{Exporter: wrapped, ch: ch}
	ch.replaySend = func(ctx context.Context, request []byte) error {
		slot := &wireSlot{replay: request}
		return wrapped.Export(withWireSlot(ctx, slot), &metricdata.ResourceMetrics{})
	}
	return e
}

func (e *persistentMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	return e.ch.export(ctx, newSDKOp(countMetricData(rm), func(ctx context.Context) error {
		return e.Exporter.Export(ctx, rm)
	}))
}

// persistentSpanExporter decorates the SDK span exporter the same way.
type persistentSpanExporter struct {
	wrapped sdktrace.SpanExporter
	ch      *wireChannel
}

func newPersistentSpanExporter(wrapped sdktrace.SpanExporter, ch *wireChannel) *persistentSpanExporter {
	e := &persistentSpanExporter{wrapped: wrapped, ch: ch}
	ch.replaySend = func(ctx context.Context, request []byte) error {
		slot := &wireSlot{replay: request}
		// The exporter sends nothing for an empty batch; one placeholder
		// span makes it build a request, which the hook replaces.
		stub := tracetest.SpanStub{Name: "replay"}.Snapshot()
		return wrapped.ExportSpans(withWireSlot(ctx, slot), []sdktrace.ReadOnlySpan{stub})
	}
	return e
}

func (e *persistentSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.ch.export(ctx, newSDKOp(len(spans), func(ctx context.Context) error {
		return e.wrapped.ExportSpans(ctx, spans)
	}))
	if errors.Is(err, errExportQueued) {
		// Safe on disk: the span processor has nothing to report.
		return nil
	}
	return err
}

func (e *persistentSpanExporter) Shutdown(ctx context.Context) error {
	return e.wrapped.Shutdown(ctx)
}

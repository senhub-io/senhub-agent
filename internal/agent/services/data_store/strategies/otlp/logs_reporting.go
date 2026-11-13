package otlp

import (
	"context"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// reportingLogExporter reports the partial_success of every logs export
// when the dead-letter queue is not in front of the exporter. Without it
// the rejection would only reach the SDK's global error handler, tagged
// as an SDK error instead of an export rejection.
type reportingLogExporter struct {
	sdklog.Exporter
	reporter *partialSuccessReporter
}

func (e *reportingLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	return e.reporter.reportRejections(e.Exporter.Export(ctx, records))
}

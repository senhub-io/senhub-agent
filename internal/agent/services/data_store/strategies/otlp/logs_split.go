package otlp

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"senhub-agent.go/internal/agent/services/exporterrors"
)

// DefaultLogsMaxExportBytes keeps one logs export under the receivers'
// 4 MiB default max message size (Toise and our own otlp_receiver), with
// a 1 MiB margin for what the estimate does not see.
const DefaultLogsMaxExportBytes = 3 << 20

const (
	recordOverheadBytes = 96
	attrOverheadBytes   = 12
)

// splittingLogExporter bounds the size of every export it forwards. The
// BatchProcessor caps a batch by record count, not bytes, so a few
// thousand entity heartbeats with long relationship arrays can exceed
// the receiver's limit — a refusal that is deterministic and never
// clears.
//
// It cuts only between records. An entity event is one record carrying
// its complete entity.relationships array, so no entity is ever split,
// and each entity is self-describing state with no snapshot-level
// delimiter: the receiver retires an entity by its own liveness, never
// by its absence from a given export.
type splittingLogExporter struct {
	sdklog.Exporter
	maxBytes int
}

func newSplittingLogExporter(next sdklog.Exporter, maxBytes int) sdklog.Exporter {
	if maxBytes <= 0 {
		maxBytes = DefaultLogsMaxExportBytes
	}
	return &splittingLogExporter{Exporter: next, maxBytes: maxBytes}
}

func (e *splittingLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	chunks := splitRecordsByBytes(records, e.maxBytes)
	if len(chunks) <= 1 {
		return e.Exporter.Export(ctx, records)
	}
	var errs []error
	for _, chunk := range chunks {
		if err := e.Exporter.Export(ctx, chunk); err != nil {
			errs = append(errs, err)
			if ctx.Err() != nil {
				break
			}
		}
	}
	// A permanent refusal wins over a transient failure: reporting the
	// transient one would have the outer queue persist the whole batch,
	// poisoned chunk included. The chunks it costs are entity heartbeats,
	// re-emitted every interval.
	for _, err := range errs {
		if !exporterrors.IsRetryable(classifyExportError(err)) {
			return err
		}
	}
	return errors.Join(errs...)
}

// splitRecordsByBytes groups consecutive records so each group's
// estimated serialized size stays within maxBytes. A single record
// larger than the budget is sent alone: it cannot be cut.
func splitRecordsByBytes(records []sdklog.Record, maxBytes int) [][]sdklog.Record {
	if len(records) == 0 {
		return nil
	}
	var chunks [][]sdklog.Record
	start, size := 0, 0
	for i := range records {
		n := estimateRecordBytes(&records[i])
		if i > start && size+n > maxBytes {
			chunks = append(chunks, records[start:i])
			start, size = i, 0
		}
		size += n
	}
	return append(chunks, records[start:])
}

// estimateRecordBytes over-approximates a record's protobuf size.
// Repeating the scope and resource per record overstates a grouped
// request, which is the safe direction.
func estimateRecordBytes(r *sdklog.Record) int {
	n := recordOverheadBytes + len(r.EventName()) + len(r.SeverityText()) + valueBytes(r.Body())
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		n += attrOverheadBytes + len(string(kv.Key)) + valueBytes(kv.Value)
		return true
	})
	scope := r.InstrumentationScope()
	n += len(scope.Name) + len(scope.Version) + len(scope.SchemaURL)
	n += attrSetBytes(scope.Attributes.ToSlice())
	if res := r.Resource(); res != nil {
		n += attrSetBytes(res.Attributes())
	}
	return n
}

func attrSetBytes(kvs []attribute.KeyValue) int {
	n := 0
	for _, kv := range kvs {
		n += attrOverheadBytes + len(string(kv.Key)) + valueBytes(kv.Value)
	}
	return n
}

func valueBytes(v attribute.Value) int {
	switch v.Type() {
	case attribute.STRING:
		return len(v.AsString())
	case attribute.EMPTY:
		return 0
	case attribute.BYTESLICE:
		return len(v.AsByteSlice())
	case attribute.SLICE:
		n := 0
		for _, e := range v.AsSlice() {
			n += attrOverheadBytes + valueBytes(e)
		}
		return n
	case attribute.MAP:
		return attrSetBytes(v.AsMap())
	default:
		return len(v.Emit()) + attrOverheadBytes
	}
}

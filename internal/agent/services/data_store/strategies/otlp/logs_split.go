package otlp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/exporterrors"
	"senhub-agent.go/internal/agent/services/logger"
)

// DefaultLogsMaxExportBytes keeps one logs export under the receivers'
// 4 MiB default max message size (Toise and our own otlp_receiver), with
// a 1 MiB margin for what the estimate does not see.
const DefaultLogsMaxExportBytes = 3 << 20

const (
	recordOverheadBytes = 96
	attrOverheadBytes   = 12
	// minLogsExportBudget stops a refusal of a small part from driving
	// the working budget to a size where every record goes alone.
	minLogsExportBudget = 64 << 10
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
//
// The budget is an estimate and the receiver's limit is not known in
// advance, so a size refusal (gRPC RESOURCE_EXHAUSTED without a throttle
// hint, HTTP 413) is answered by halving the part down to one record
// and lowering the working budget for the next batches. A single record
// still refused is dropped and counted: nothing smaller can be sent.
type splittingLogExporter struct {
	sdklog.Exporter
	maxBytes int

	// budget is the working limit: maxBytes until the receiver refuses
	// an export for its size.
	budget         atomic.Int64
	budgetLogged   atomic.Bool
	tooLargeLogged atomic.Bool
	log            *logger.ModuleLogger
}

func newSplittingLogExporter(next sdklog.Exporter, maxBytes int, log *logger.ModuleLogger) sdklog.Exporter {
	if maxBytes <= 0 {
		maxBytes = DefaultLogsMaxExportBytes
	}
	e := &splittingLogExporter{
		Exporter: next,
		maxBytes: maxBytes,
		log:      log,
	}
	e.budget.Store(int64(maxBytes))
	return e
}

func (e *splittingLogExporter) workingBudget() int {
	return int(e.budget.Load())
}

func (e *splittingLogExporter) dropRecordTooLarge(r *sdklog.Record) {
	agentstate.IncrementOTLPDropped("record_too_large")
	if e.log == nil || !e.tooLargeLogged.CompareAndSwap(false, true) {
		return
	}
	e.log.Warn().
		Int("estimated_bytes", estimateRecordBytes(r)).
		Str("event_name", r.EventName()).
		Str("scope", r.InstrumentationScope().Name).
		Msg("OTLP logs record larger than the receiver accepts; dropped (counted in senhub_agent_otlp_dropped_total{reason=\"record_too_large\"})")
}

// lowerBudget brings the working limit to half of what was refused, so
// the next batches are cut small enough from the start instead of being
// halved again on every export. It never rises: the receiver's limit
// does not change while the agent runs.
func (e *splittingLogExporter) lowerBudget(refused int) {
	for {
		current := e.budget.Load()
		floor := min(int64(minLogsExportBudget), current)
		candidate := max(int64(refused/2), floor)
		if candidate >= current {
			return
		}
		if !e.budget.CompareAndSwap(current, candidate) {
			continue
		}
		if e.log != nil && e.budgetLogged.CompareAndSwap(false, true) {
			e.log.Warn().
				Int("configured_bytes", e.maxBytes).
				Int64("working_bytes", candidate).
				Int("refused_estimated_bytes", refused).
				Msg("OTLP logs export refused for size by the receiver; working export size limit lowered (set signals.logs.max_export_bytes below the receiver limit to avoid the first refusal)")
		}
		return
	}
}

// splitExportError tells the queue which records of a split export were not delivered.
type splitExportError struct {
	err         error
	undelivered []sdklog.Record // failed transiently: the only records to keep
	rejected    int             // refused on their merits (not for size)
	dropped     int             // single records too large for the receiver
}

func (e *splitExportError) Error() string { return e.err.Error() }
func (e *splitExportError) Unwrap() error { return e.err }

var errSplitCallCap = errors.New("logs export split: call budget exhausted")

// isSizeRefusal reports a refusal of these exact bytes for their size:
// gRPC RESOURCE_EXHAUSTED without a throttle hint, or HTTP 413. A
// smaller export of the same records can still be accepted.
func isSizeRefusal(err error) bool {
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return st.Code() == codes.ResourceExhausted && !hasRetryInfo(st)
	}
	code, ok := httpStatusFromError(err)
	if !ok {
		return false
	}
	// The OpenTelemetry Collector answers a body over its
	// max_request_body_size with 400, its message saying so, not 413.
	return code == http.StatusRequestEntityTooLarge ||
		(code == http.StatusBadRequest && strings.Contains(err.Error(), "request body too large"))
}

// splitRun is the outcome of one Export across its parts.
type splitRun struct {
	calls, maxCalls int
	undelivered     []sdklog.Record
	reports         []error
	failures        []error
	rejected        int
	dropped         int
}

func (e *splittingLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	chunks := splitRecordsByBytes(records, e.workingBudget())
	run := &splitRun{maxCalls: 2 * len(records)}
	if len(chunks) <= 1 {
		run.calls++
		err := e.Exporter.Export(ctx, records)
		if err == nil {
			return nil
		}
		found, rest := splitPartialSuccess(err)
		if rest == nil || !isSizeRefusal(rest) || len(records) == 0 {
			return err
		}
		if len(found) > 0 {
			run.reports = append(run.reports, err)
		}
		e.refusedForSize(ctx, records, run)
		return run.result()
	}
	for _, chunk := range chunks {
		e.deliver(ctx, chunk, run)
	}
	return run.result()
}

// deliver sends one part and halves it on a size refusal, down to a
// single record, which is dropped: nothing smaller can be sent.
func (e *splittingLogExporter) deliver(ctx context.Context, part []sdklog.Record, run *splitRun) {
	if err := ctx.Err(); err != nil {
		run.undelivered = append(run.undelivered, part...)
		run.failures = append(run.failures, err)
		return
	}
	if run.calls >= run.maxCalls {
		run.undelivered = append(run.undelivered, part...)
		run.failures = append(run.failures, errSplitCallCap)
		return
	}
	run.calls++
	err := e.Exporter.Export(ctx, part)
	if err == nil {
		return
	}
	found, rest := splitPartialSuccess(err)
	if len(found) > 0 {
		run.reports = append(run.reports, err)
	}
	switch {
	case rest == nil:
	case isSizeRefusal(rest):
		e.refusedForSize(ctx, part, run)
	case exporterrors.IsRetryable(classifyExportError(rest)):
		run.undelivered = append(run.undelivered, part...)
		run.failures = append(run.failures, rest)
	default:
		run.rejected += len(part)
		run.failures = append(run.failures, rest)
	}
}

func (e *splittingLogExporter) refusedForSize(ctx context.Context, part []sdklog.Record, run *splitRun) {
	if len(part) == 1 {
		e.dropRecordTooLarge(&part[0])
		run.dropped++
		return
	}
	size := 0
	for i := range part {
		size += estimateRecordBytes(&part[i])
	}
	e.lowerBudget(size)
	mid := len(part) / 2
	e.deliver(ctx, part[:mid], run)
	e.deliver(ctx, part[mid:], run)
}

// result keeps the SDK's error shape when nothing is left to act on, so
// a delivered export with partial-success reports reads as it does
// without the splitter.
func (run *splitRun) result() error {
	if len(run.undelivered) == 0 && run.rejected == 0 {
		return errors.Join(run.reports...)
	}
	return &splitExportError{
		err:         errors.Join(append(run.reports, run.failures...)...),
		undelivered: run.undelivered,
		rejected:    run.rejected,
		dropped:     run.dropped,
	}
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
		return len(v.String()) + attrOverheadBytes
	}
}

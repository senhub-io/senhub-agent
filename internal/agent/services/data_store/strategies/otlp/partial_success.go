package otlp

import (
	"errors"
	"regexp"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// A consumer that keeps only part of a batch answers OK and describes
// what it refused in the response's `partial_success` field. The SDK
// surfaces that as an ERROR returned from Export, joined with any real
// failure — which puts "the collector refused six records" on the same
// path as "the collector is unreachable". That is wrong in both
// directions, and expensively so for the logs signal:
//
//   - The batch WAS delivered. Classified as a transport failure it goes
//     to the dead-letter queue, is replayed at boot and on every
//     recovery, and re-delivers every record the consumer already
//     accepted while the refused ones are refused again. The queue then
//     evicts oldest-first to make room — real event logs dropped to keep
//     re-sending a payload nothing will ever take (#833 made that
//     argument for HTTP 400; a partial success slipped past it as
//     "transport").
//   - The refused records ARE lost, and nothing counted them. From the
//     producing host the export looks like a plain failure that later
//     recovers; the reason lives one hop away, in the consumer's journal.
//     That is how a production fan-out lost six entity records per batch
//     for thirty-seven minutes (#819).
//
// So a partial success is split out of the export error before it is
// classified: counted per record, reported once per window, and NOT
// persisted.
//
// The trace signal never reaches our code — the SDK's own processors
// hand their errors to the global OTel error handler, which was never
// installed, so they landed on the standard library's logger: stderr,
// not the file the agent writes. installSDKErrorHandler closes that.

// partialSuccessWindow bounds how often one distinct rejection message
// gets a log line. Repetition carries no new information — the same
// consumer refusing the same thing on every batch is one condition, not
// four thousand events — and the counter behind it stays exact either
// way.
const partialSuccessWindow = time.Minute

// partialSuccessRunCap bounds the coalescing map. The key holds a
// message written by the consumer, so a far end that embeds a record id
// in its rejection would otherwise grow the map without limit. Past the
// cap the state is dropped and coalescing restarts: worse log
// compression for one window, never unbounded memory.
const partialSuccessRunCap = 256

// partialSuccessNouns maps the noun the SDK puts in its message to the
// signal name used in metrics and logs. The SDK's wording is the only
// thing that says which pipeline was refused when the error arrives on
// the process-wide handler, with no other context.
var partialSuccessNouns = map[string]string{
	"logs":               "logs",
	"log records":        "logs",
	"metric data points": "metrics",
	"spans":              "traces",
}

// partialSuccessRe matches the message every SDK exporter builds for a
// partial success. The consumer's own text may contain parentheses, so
// the count is anchored on the end of the string rather than on the
// first opening bracket.
var partialSuccessRe = regexp.MustCompile(`^OTLP partial success: (.*) \((-?\d+) ([a-z ]+) rejected\)$`)

type partialSuccess struct {
	signal   string
	rejected int64
	message  string
}

// parsePartialSuccess recognises an SDK partial-success error by its
// message. The SDK type carrying it lives in an internal package, so
// errors.As cannot reach it and the text is the only handle. A wording
// change upstream therefore degrades to today's behaviour — the error
// stays whole and is classified as before — never to a wrong count.
func parsePartialSuccess(msg string) (partialSuccess, bool) {
	m := partialSuccessRe.FindStringSubmatch(msg)
	if m == nil {
		return partialSuccess{}, false
	}
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return partialSuccess{}, false
	}
	signal, known := partialSuccessNouns[m[3]]
	if !known {
		// A signal the SDK grows after this was written still deserves to
		// be seen; only its label is unknown.
		signal = "unknown"
	}
	return partialSuccess{signal: signal, rejected: n, message: m[1]}, true
}

// splitPartialSuccess separates a consumer's rejections from a genuine
// export failure. The SDK joins the two — a batch can be partly refused
// AND the call can fail — so the caller gets what it must count and,
// separately, the error it must still act on. A nil error back means
// the export landed and only part of it was kept.
//
// It walks the whole tree, joins and wrappers alike, because that is how
// the rejection actually arrives: the metric exporter joins the partial
// success into its upload error and the strategy then wraps the result
// ("export: failed to upload metrics: OTLP partial success: …"). Reading
// only the top-level message finds the logs rail and misses the metrics
// one.
//
// A wrapper's own context is dropped from what comes back when its cause
// held a rejection AND a failure — a shape the SDK does not currently
// produce — which loses a prefix, never an error.
func splitPartialSuccess(err error) ([]partialSuccess, error) {
	if err == nil {
		return nil, nil
	}
	if ps, ok := parsePartialSuccess(err.Error()); ok {
		return []partialSuccess{ps}, nil
	}

	// The two shapes are the standard library's own: errors.Join gives
	// the first, fmt.Errorf("%w") the second. This walks the tree rather
	// than matching a type, so errors.As has nothing to offer here — the
	// value being looked for is unreachable, in an SDK-internal package.
	if joined, ok := err.(interface{ Unwrap() []error }); ok { //nolint:errorlint // structural walk, not a type match
		var found []partialSuccess
		var rest []error
		for _, inner := range joined.Unwrap() {
			ps, remainder := splitPartialSuccess(inner)
			found = append(found, ps...)
			if remainder != nil {
				rest = append(rest, remainder)
			}
		}
		return found, errors.Join(rest...)
	}

	if inner := errors.Unwrap(err); inner != nil {
		found, rest := splitPartialSuccess(inner)
		if len(found) == 0 {
			// Nothing to take out: hand back the error as written, with
			// the context its wrappers added.
			return nil, err
		}
		return found, rest
	}

	return nil, err
}

type reportKey struct {
	signal  string
	message string
}

type reportRun struct {
	lastLogged  time.Time
	occurrences uint64
	rejected    int64
}

// partialSuccessReporter counts every rejection and writes at most one
// line per distinct message per window.
type partialSuccessReporter struct {
	logger *logger.ModuleLogger
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	runs map[reportKey]*reportRun
}

func newPartialSuccessReporter(moduleLogger *logger.ModuleLogger) *partialSuccessReporter {
	return &partialSuccessReporter{
		logger: moduleLogger,
		window: partialSuccessWindow,
		now:    time.Now,
		runs:   map[reportKey]*reportRun{},
	}
}

// admit reports whether this occurrence gets a log line, and what the
// suppressed run behind it amounted to. The tail of a run that stops is
// never written — the next occurrence is what flushes it — which is why
// the counter, not the log, is the authority on how much was lost.
func (r *partialSuccessReporter) admit(k reportKey, rejected int64) (occurrences uint64, suppressedRejected int64, log bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if len(r.runs) > partialSuccessRunCap {
		r.runs = map[reportKey]*reportRun{}
	}

	run, seen := r.runs[k]
	if !seen {
		r.runs[k] = &reportRun{lastLogged: now}
		return 0, 0, true
	}
	if now.Sub(run.lastLogged) < r.window {
		run.occurrences++
		run.rejected += rejected
		return 0, 0, false
	}
	occurrences, suppressedRejected = run.occurrences, run.rejected
	run.lastLogged, run.occurrences, run.rejected = now, 0, 0
	return occurrences, suppressedRejected, true
}

// reportRejection records the records a consumer refused and says so
// once per window.
func (r *partialSuccessReporter) reportRejection(ps partialSuccess) {
	agentstate.IncrementExportRejected(strategyName, ps.signal, ps.rejected)

	occurrences, suppressedRejected, log := r.admit(reportKey{signal: ps.signal, message: ps.message}, ps.rejected)
	if !log || r.logger == nil {
		return
	}
	ev := r.logger.Warn().
		Str("signal", ps.signal).
		Int64("rejected", ps.rejected).
		Str("consumer_message", redactSensitive(ps.message))
	if occurrences > 0 {
		ev = ev.Uint64("suppressed_batches", occurrences).
			Int64("suppressed_rejected", suppressedRejected)
	}
	ev.Msg("consumer accepted the batch and refused part of it — those records are lost, not retried")
}

// reportRejections is the call every export path makes: it counts what
// the consumer refused and hands back the error that is still a failure.
func (r *partialSuccessReporter) reportRejections(err error) error {
	rejections, rest := splitPartialSuccess(err)
	for _, ps := range rejections {
		r.reportRejection(ps)
	}
	return rest
}

// reportSDKError surfaces an SDK error that is not a partial success.
// The export paths log their own failures, so a line here can double one
// of those; the coalescing keeps that to one per message per window, and
// the errors nothing else reports — the trace pipeline, records the log
// processor drops before any exporter sees them — are worth the overlap.
func (r *partialSuccessReporter) reportSDKError(err error) {
	occurrences, _, log := r.admit(reportKey{signal: "", message: err.Error()}, 0)
	if !log || r.logger == nil {
		return
	}
	ev := r.logger.Warn().Str("error", redactSensitive(err.Error()))
	if occurrences > 0 {
		ev = ev.Uint64("suppressed_occurrences", occurrences)
	}
	ev.Msg("OpenTelemetry SDK reported an error")
}

// sdkErrorHandler adapts the reporter to otel.ErrorHandler.
type sdkErrorHandler struct {
	reporter *partialSuccessReporter
}

func (h *sdkErrorHandler) Handle(err error) {
	if err == nil {
		return
	}
	if rest := h.reporter.reportRejections(err); rest != nil {
		h.reporter.reportSDKError(rest)
	}
}

// sdkErrorHandlerOnce guards the process-wide handler. The handler is
// global to the OTel SDK, so a second strategy — a failover target, a
// reload — must not replace the one already reporting.
var sdkErrorHandlerOnce sync.Once

// installSDKErrorHandler routes the SDK's internal errors, partial
// successes included, into the agent's log.
func installSDKErrorHandler(moduleLogger *logger.ModuleLogger) {
	sdkErrorHandlerOnce.Do(func() {
		otel.SetErrorHandler(&sdkErrorHandler{reporter: newPartialSuccessReporter(moduleLogger)})
	})
}

// resetSDKErrorHandlerForTest re-arms the guard. Test-only: the guard is
// process-wide, so any test that has already started a strategy would
// otherwise turn the installation into a no-op.
func resetSDKErrorHandlerForTest() {
	sdkErrorHandlerOnce = sync.Once{}
}

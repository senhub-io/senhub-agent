package otlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/logger"
)

// syncBuffer collects log output. zerolog writes from whatever goroutine
// logged, and the SDK handler runs on the exporting one.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func bufferModuleLogger(w *syncBuffer) *logger.ModuleLogger {
	zl := zerolog.New(w)
	return logger.NewModuleLogger(&zl, "test.partialsuccess")
}

// ── the message the SDK hands to the error handler ───────────────────

// TestParsePartialSuccessReadsEverySDKWording pins the parser against
// the exact strings the exporters build. They are the only handle we
// have: the SDK type carrying a partial success lives in an internal
// package, so errors.As cannot reach it and the text is the contract.
func TestParsePartialSuccessReadsEverySDKWording(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		signal   string
		rejected int64
		message  string
	}{
		{
			// internal.LogPartialSuccessError, the wording of the version
			// this depends on.
			name:     "logs",
			in:       `OTLP partial success: invalid entity record: unknown relationship.type "has_segment" (6 logs rejected)`,
			signal:   "logs",
			rejected: 6,
			message:  `invalid entity record: unknown relationship.type "has_segment"`,
		},
		{
			// The wording an older exporter formatted itself. Kept so a
			// downgrade, or a dependency that lags, still counts.
			name:     "logs, older wording",
			in:       "OTLP partial success: rejected (6 log records rejected)",
			signal:   "logs",
			rejected: 6,
			message:  "rejected",
		},
		{
			// internal.MetricPartialSuccessError.
			name:     "metrics",
			in:       "OTLP partial success: series limit reached (12 metric data points rejected)",
			signal:   "metrics",
			rejected: 12,
			message:  "series limit reached",
		},
		{
			// internal.TracePartialSuccessError.
			name:     "traces",
			in:       "OTLP partial success: sampler dropped (3 spans rejected)",
			signal:   "traces",
			rejected: 3,
			message:  "sampler dropped",
		},
		{
			// The consumer writes that message, so it may carry its own
			// brackets. Anchoring on the first "(" would cut it in half
			// and lose the count.
			name:     "consumer message contains parentheses",
			in:       "OTLP partial success: rejected by rule (r-14) on tenant acme (2 logs rejected)",
			signal:   "logs",
			rejected: 2,
			message:  "rejected by rule (r-14) on tenant acme",
		},
		{
			// A count with no message from the consumer.
			name:     "empty consumer message",
			in:       "OTLP partial success:  (6 logs rejected)",
			signal:   "logs",
			rejected: 6,
			message:  "",
		},
		{
			// The SDK's own fallback when the consumer sends a message
			// and no count.
			name:     "message with no count",
			in:       "OTLP partial success: empty message (0 spans rejected)",
			signal:   "traces",
			rejected: 0,
			message:  "empty message",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePartialSuccess(c.in)
			if !ok {
				t.Fatalf("parsePartialSuccess(%q) did not recognise an SDK partial success", c.in)
			}
			if got.signal != c.signal {
				t.Errorf("signal = %q, want %q", got.signal, c.signal)
			}
			if got.rejected != c.rejected {
				t.Errorf("rejected = %d, want %d", got.rejected, c.rejected)
			}
			if got.message != c.message {
				t.Errorf("message = %q, want %q", got.message, c.message)
			}
		})
	}
}

// TestParsePartialSuccessIgnoresOtherErrors keeps the ordinary SDK
// errors — connection refused, context deadline — out of the rejection
// counter. Counting them there would make an unreachable consumer look
// like a rejecting one, which points an operator at the wrong end.
func TestParsePartialSuccessIgnoresOtherErrors(t *testing.T) {
	for _, in := range []string{
		"context deadline exceeded",
		"rpc error: code = Unavailable desc = connection refused",
		"OTLP partial success",
		"exporter export: OTLP partial success: x (2 widgets rejected) trailing",
	} {
		if _, ok := parsePartialSuccess(in); ok {
			t.Errorf("parsePartialSuccess(%q) claimed a partial success", in)
		}
	}
}

// TestUnknownSignalStillCounts: a signal the SDK grows later must not
// fall on the floor because its noun is not in the table.
func TestUnknownSignalStillCounts(t *testing.T) {
	got, ok := parsePartialSuccess("OTLP partial success: nope (4 profiles rejected)")
	if !ok {
		t.Fatal("an unfamiliar noun made the whole message unparseable")
	}
	if got.signal != "unknown" || got.rejected != 4 {
		t.Errorf("got %+v, want signal=unknown rejected=4", got)
	}
}

// TestSplitPartialSuccessWalksTheErrorTree is the case a first version
// of this got wrong. The rejection does not arrive as the error: the
// metric exporter joins it into its upload error and the strategy wraps
// that again, so a reader that only looks at the top-level message sees
// the logs rail and misses the metrics one entirely.
func TestSplitPartialSuccessWalksTheErrorTree(t *testing.T) {
	rejection := errors.New("OTLP partial success: series limit reached (12 metric data points rejected)")
	transport := errors.New("rpc error: code = Unavailable desc = connection refused")

	t.Run("wrapped by the export path", func(t *testing.T) {
		err := fmt.Errorf("export: failed to upload metrics: %w", errors.Join(rejection))
		found, rest := splitPartialSuccess(err)
		if len(found) != 1 || found[0].rejected != 12 {
			t.Fatalf("found %+v, want one rejection of 12 datapoints", found)
		}
		if rest != nil {
			t.Errorf("a delivered push left %v behind — it will be reported as a failed export", rest)
		}
	})

	t.Run("refused and failed at once", func(t *testing.T) {
		found, rest := splitPartialSuccess(errors.Join(rejection, transport))
		if len(found) != 1 {
			t.Fatalf("found %+v, want the rejection", found)
		}
		if rest == nil || !strings.Contains(rest.Error(), "connection refused") {
			t.Errorf("rest = %v — a real failure must survive the split", rest)
		}
	})

	t.Run("nothing to take out", func(t *testing.T) {
		err := fmt.Errorf("export: %w", transport)
		found, rest := splitPartialSuccess(err)
		if len(found) != 0 {
			t.Errorf("invented %+v out of a transport error", found)
		}
		if rest == nil || !strings.Contains(rest.Error(), "export: ") {
			t.Errorf("rest = %v — the wrapper's context was dropped from an error that held no rejection", rest)
		}
	})
}

// ── counting and coalescing ──────────────────────────────────────────

// TestEveryRejectionIsCountedEvenWhenTheLineIsSuppressed is the property
// the whole design rests on: the log is compressed, the counter is not.
// If both were compressed, the operator would have no exact number
// anywhere.
func TestEveryRejectionIsCountedEvenWhenTheLineIsSuppressed(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	var buf syncBuffer
	r := newPartialSuccessReporter(bufferModuleLogger(&buf))
	now := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return now }

	for i := 0; i < 50; i++ {
		r.reportRejection(partialSuccess{signal: "logs", rejected: 6, message: "unknown relationship.type"})
		now = now.Add(time.Second)
	}

	var total uint64
	for _, row := range agentstate.GetExportRejected() {
		if row.Strategy == strategyName && row.Signal == "logs" {
			total = row.Count
		}
	}
	if total != 300 {
		t.Errorf("counted %d rejected records, want 300 (50 batches × 6)", total)
	}

	lines := strings.Count(buf.String(), "\n")
	if lines != 1 {
		t.Errorf("wrote %d log lines for 50 identical rejections inside one window, want 1", lines)
	}
}

// TestTheNextWindowReportsWhatWasSuppressed: coalescing that silently
// discarded the run would understate a long outage. The line that opens
// a new window carries the batches and the records the previous one held
// back.
func TestTheNextWindowReportsWhatWasSuppressed(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	var buf syncBuffer
	r := newPartialSuccessReporter(bufferModuleLogger(&buf))
	now := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return now }

	r.reportRejection(partialSuccess{signal: "logs", rejected: 6, message: "same message"}) // logged, opens the window
	for i := 0; i < 9; i++ {
		now = now.Add(time.Second)
		r.reportRejection(partialSuccess{signal: "logs", rejected: 6, message: "same message"}) // suppressed
	}
	now = now.Add(2 * partialSuccessWindow)
	r.reportRejection(partialSuccess{signal: "logs", rejected: 6, message: "same message"}) // logged again

	out := buf.String()
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("want 2 lines (one per window), got:\n%s", out)
	}
	if !strings.Contains(out, `"suppressed_batches":9`) {
		t.Errorf("the second line does not say how many batches it stood for:\n%s", out)
	}
	if !strings.Contains(out, `"suppressed_rejected":54`) {
		t.Errorf("the second line does not say how many records were lost meanwhile:\n%s", out)
	}
}

// TestDistinctMessagesAreReportedSeparately: two different rejection
// reasons are two different problems, and one must not mask the other.
func TestDistinctMessagesAreReportedSeparately(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	var buf syncBuffer
	r := newPartialSuccessReporter(bufferModuleLogger(&buf))
	now := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return now }

	r.reportRejection(partialSuccess{signal: "logs", rejected: 1, message: "unknown relationship.type"})
	r.reportRejection(partialSuccess{signal: "logs", rejected: 1, message: "entity id too long"})
	r.reportRejection(partialSuccess{signal: "metrics", rejected: 1, message: "unknown relationship.type"})

	if got := strings.Count(buf.String(), "\n"); got != 3 {
		t.Errorf("wrote %d lines for three distinct rejections, want 3:\n%s", got, buf.String())
	}
}

// TestCoalescingStateIsBounded: the key holds text the consumer wrote,
// so a far end embedding a record id in every rejection would otherwise
// grow the map for the life of the process.
func TestCoalescingStateIsBounded(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	var buf syncBuffer
	r := newPartialSuccessReporter(bufferModuleLogger(&buf))

	for i := 0; i < partialSuccessRunCap*3; i++ {
		r.reportRejection(partialSuccess{signal: "logs", rejected: 1, message: "record " + strconv.Itoa(i)})
	}
	if len(r.runs) > partialSuccessRunCap+1 {
		t.Errorf("coalescing map holds %d entries, cap is %d", len(r.runs), partialSuccessRunCap)
	}
}

// TestNonPartialSDKErrorsSurfaceToo: before a handler existed these went
// to the standard library logger, which for a service unit means the
// journal — not the file the operator is told to read.
func TestNonPartialSDKErrorsSurfaceToo(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	var buf syncBuffer
	h := &sdkErrorHandler{reporter: newPartialSuccessReporter(bufferModuleLogger(&buf))}

	h.Handle(errors.New("periodic reader export: context deadline exceeded"))

	if !strings.Contains(buf.String(), "context deadline exceeded") {
		t.Errorf("an SDK error was swallowed:\n%s", buf.String())
	}
	if rows := agentstate.GetExportRejected(); len(rows) != 0 {
		t.Errorf("a transport error was counted as a rejection: %+v", rows)
	}

	h.Handle(nil) // must not panic
}

// ── the wiring, end to end ───────────────────────────────────────────

// rejectingLogsServer answers OK and refuses part of the batch, the way
// a consumer that does not know a relationship type does.
type rejectingLogsServer struct {
	collectorlogspb.UnimplementedLogsServiceServer
}

func (s *rejectingLogsServer) Export(
	_ context.Context,
	_ *collectorlogspb.ExportLogsServiceRequest,
) (*collectorlogspb.ExportLogsServiceResponse, error) {
	return &collectorlogspb.ExportLogsServiceResponse{
		PartialSuccess: &collectorlogspb.ExportLogsPartialSuccess{
			RejectedLogRecords: 6,
			ErrorMessage:       `invalid entity record: unknown relationship.type "has_segment"`,
		},
	}, nil
}

func startRejectingLogsServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	collectorlogspb.RegisterLogsServiceServer(srv, &rejectingLogsServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// TestPartialSuccessIsNotAFailedExport is the regression for #819, on a
// real gRPC exporter talking to a consumer that refuses part of the
// batch.
//
// Four things have to hold at once, and before this fix none of them
// did: the loss is counted, the consumer's reason reaches the agent's
// own log, the delivered batch does NOT go to the dead-letter queue, and
// the exporter stays healthy. The queue assertion is the expensive one —
// persisting a delivered batch means replaying at boot and on every
// recovery, re-sending records the consumer already accepted, while
// oldest-first eviction drops event logs that could still be delivered.
func TestPartialSuccessIsNotAFailedExport(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	realExporter, err := buildLogExporter(ctx, relayTestConfig(startRejectingLogsServer(t), "grpc"))
	if err != nil {
		t.Fatalf("buildLogExporter: %v", err)
	}
	t.Cleanup(func() { _ = realExporter.Shutdown(context.Background()) })

	var buf syncBuffer
	dir := t.TempDir()
	queue := newLogsQueue(dir, 0, testModuleLogger(t))
	exporter := newPersistentLogExporter(realExporter, queue, bufferModuleLogger(&buf))

	// Through the real pipeline: records only serialize into the queue
	// when they carry the pipeline's scope, so a hand-built record would
	// make the "nothing persisted" assertion pass for the wrong reason.
	pipe := buildLogsPipeline(
		exporter,
		resource.NewSchemaless(),
		LogsSignal{BufferSize: 100, BatchSize: 1, BatchTimeout: time.Hour},
		"test",
	)
	pipe.emit(ctx, agentstate.LogRecord{
		Timestamp:         time.Unix(1_700_000_000, 0),
		Severity:          9,
		SeverityText:      "INFO",
		Body:              "record the consumer partly refuses",
		ProducerProbeName: "syslog",
	})
	_ = pipe.provider.ForceFlush(ctx)

	var counted uint64
	for _, row := range agentstate.GetExportRejected() {
		if row.Signal == "logs" {
			counted = row.Count
		}
	}
	if counted != 6 {
		t.Errorf("export.rejected{signal=logs} = %d, want 6", counted)
	}

	out := buf.String()
	if !strings.Contains(out, "has_segment") {
		t.Errorf("the consumer's reason never reached the agent log:\n%s", out)
	}

	if files := countQueueFiles(t, dir); files != 0 {
		t.Errorf("a delivered batch was persisted to the dead-letter queue (%d files) — it will be replayed against a consumer that already has it", files)
	}
	if !exporter.healthy.Load() {
		t.Error("the exporter marked itself unhealthy, which triggers recovery replays of a batch that was never lost")
	}
}

// rejectingMetricsServer accepts the push and refuses part of it, the
// way a backend at its series limit does.
type rejectingMetricsServer struct {
	collectormetricspb.UnimplementedMetricsServiceServer
}

func (s *rejectingMetricsServer) Export(
	_ context.Context,
	_ *collectormetricspb.ExportMetricsServiceRequest,
) (*collectormetricspb.ExportMetricsServiceResponse, error) {
	return &collectormetricspb.ExportMetricsServiceResponse{
		PartialSuccess: &collectormetricspb.ExportMetricsPartialSuccess{
			RejectedDataPoints: 12,
			ErrorMessage:       "series limit reached",
		},
	}, nil
}

func startRejectingMetricsServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	collectormetricspb.RegisterMetricsServiceServer(srv, &rejectingMetricsServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// TestMetricsRejectionIsNotAnExportFailure is the metrics half. The
// consequence differs from the logs rail — there is no queue behind
// metrics, the LWW store supersedes a rejected push on its own — but the
// reading does not: an export error says "nothing got through", and here
// everything but twelve datapoints did. An operator paging on the export
// error rate would be paging on the wrong signal.
func TestMetricsRejectionIsNotAnExportFailure(t *testing.T) {
	agentstate.ResetExportRejectedForTest()

	s := newTestStrategy(t, map[string]interface{}{
		"endpoint": startRejectingMetricsServer(t),
		"tls":      map[string]interface{}{"enabled": false},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	before := agentstate.GetOTLPExportErrorsBySignal()["metrics"]
	s.doPush(ctx, []otelmapper.OtelRecord{{
		Name:  "senhub.agent.test.counter",
		Unit:  "{item}",
		Type:  "counter",
		Value: 1,
	}})

	var counted uint64
	for _, row := range agentstate.GetExportRejected() {
		if row.Signal == "metrics" {
			counted = row.Count
		}
	}
	if counted != 12 {
		t.Errorf("export.rejected{signal=metrics} = %d, want 12", counted)
	}
	if after := agentstate.GetOTLPExportErrorsBySignal()["metrics"]; after != before {
		t.Errorf("a delivered push counted as an export error (%d → %d)", before, after)
	}
}

// TestStrategyStartInstallsTheErrorHandler pins the wiring itself. The
// handler is process-wide and installed nowhere else, so a strategy that
// starts without installing it is a strategy exporting blind — which is
// exactly the state the field was in.
func TestStrategyStartInstallsTheErrorHandler(t *testing.T) {
	previous := otel.GetErrorHandler()
	t.Cleanup(func() { otel.SetErrorHandler(previous) })
	resetSDKErrorHandlerForTest()

	s := newTestStrategy(t, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	if _, ok := otel.GetErrorHandler().(*sdkErrorHandler); !ok {
		t.Fatalf("global OTel error handler is %T, want *sdkErrorHandler", otel.GetErrorHandler())
	}
}

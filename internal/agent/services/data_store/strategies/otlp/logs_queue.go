package otlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/exporterrors"
	"senhub-agent.go/internal/agent/services/logger"
)

// On-disk durability for the OTLP logs signal (issue #217).
//
// Metrics already survive a backend outage via the LWW checkpoint
// (checkpoint.go). Logs went through the SDK BatchProcessor straight to
// the exporter — a backend outage long enough to exhaust the SDK retry
// dropped them. This adds a dead-letter queue: when Export fails, the
// affected log records are serialised to disk; they are re-emitted at
// boot and whenever the backend recovers, so an outage no longer loses
// event logs.
//
// Scope: only ordinary event logs (logsScopeName) are persisted. Entity
// events (entitiesScopeName) are deliberately NOT queued — they are a
// state stream re-emitted in full at every heartbeat, so an outage is
// caught up on the next sweep without a durable queue, and their
// structured (kvlist/array) attributes would need a far heavier
// serialisation. Event logs carry only string attributes, so the
// on-disk shape stays simple.
//
// Guarantee: at-least-once. A queued batch leaves the disk only after the
// backend acknowledged an export of exactly that batch: replay calls the
// exporter directly and learns the outcome, instead of handing records
// back to the batching pipeline and deleting the file at once. A record
// may therefore be delivered twice (the backend took it but the ack was
// lost), and replayed batches can arrive after newer live ones; consumers
// order by the record timestamp and dedupe by (timestamp, body,
// attributes). While the backend is known down, new batches go straight
// to disk and only a backoff probe touches the network. On a graceful
// stop with the backend down the records still in the SDK batch are
// persisted too. The residual loss window is a hard crash (kill -9, power
// cut) while records sit in the SDK's in-memory batch, bounded by the
// batch interval, and any outage longer than the retention or the size cap.

const (
	logsQueueDirName = "logqueue"
	// logsQueueFileVersion stamps the on-disk batch shape; a reader that
	// sees a higher version skips the file rather than mis-decoding it.
	logsQueueFileVersion = 1
	// defaultLogsQueueMaxBytes caps the queue's disk footprint. Past it,
	// the oldest batches are evicted (drop-oldest) with reason
	// "logs_queue_full".
	defaultLogsQueueMaxBytes int64 = 128 << 20 // 128 MiB
)

// persistedLogRecord is the JSON projection of one event log.Record.
// The body is a string; attributes are strings, integers or floats, the
// shapes logsPipeline.emit produces.
type persistedLogRecord struct {
	TimestampUnixNano         int64              `json:"ts"`
	ObservedTimestampUnixNano int64              `json:"obs_ts,omitempty"`
	SeverityNumber            int                `json:"sev"`
	SeverityText              string             `json:"sev_text,omitempty"`
	Body                      string             `json:"body"`
	Attributes                map[string]string  `json:"attrs,omitempty"`
	EventName                 string             `json:"event,omitempty"`
	IntAttributes             map[string]int64   `json:"int_attrs,omitempty"`
	FloatAttributes           map[string]float64 `json:"float_attrs,omitempty"`
	BoolAttributes            map[string]bool    `json:"bool_attrs,omitempty"`
}

// logBatch is the on-disk envelope (one file per failed export batch).
type logBatch struct {
	Version int                  `json:"version"`
	SavedAt time.Time            `json:"saved_at"`
	Records []persistedLogRecord `json:"records"`
}

// serializeEventLog projects an SDK log record into a persistedLogRecord.
// Returns ok=false for anything that is not an ordinary event log
// (notably entity events), which the queue does not persist.
func serializeEventLog(r sdklog.Record) (persistedLogRecord, bool) {
	if r.InstrumentationScope().Name != logsScopeName {
		return persistedLogRecord{}, false
	}
	out := persistedLogRecord{
		TimestampUnixNano: r.Timestamp().UnixNano(),
		SeverityNumber:    int(r.Severity()),
		SeverityText:      r.SeverityText(),
		Body:              r.Body().AsString(),
	}
	if obs := r.ObservedTimestamp(); !obs.IsZero() {
		out.ObservedTimestampUnixNano = obs.UnixNano()
	}
	out.EventName = r.EventName()
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		key := string(kv.Key)
		switch kv.Value.Type() {
		case attribute.INT64:
			if out.IntAttributes == nil {
				out.IntAttributes = map[string]int64{}
			}
			out.IntAttributes[key] = kv.Value.AsInt64()
		case attribute.FLOAT64:
			if out.FloatAttributes == nil {
				out.FloatAttributes = map[string]float64{}
			}
			out.FloatAttributes[key] = kv.Value.AsFloat64()
		case attribute.BOOL:
			if out.BoolAttributes == nil {
				out.BoolAttributes = map[string]bool{}
			}
			out.BoolAttributes[key] = kv.Value.AsBool()
		default:
			if out.Attributes == nil {
				out.Attributes = map[string]string{}
			}
			out.Attributes[key] = kv.Value.AsString()
		}
		return true
	})
	return out, true
}

// logsQueue is the on-disk dead-letter store: one JSON file per failed
// batch under <path>/logqueue/. Files are named monotonically so a
// lexical sort yields FIFO replay order.
type logsQueue struct {
	diskQueue
}

func newLogsQueue(path string, maxBytes int64, log *logger.ModuleLogger) *logsQueue {
	if maxBytes <= 0 {
		maxBytes = defaultLogsQueueMaxBytes
	}
	q := &logsQueue{}
	q.dir = filepath.Join(path, logsQueueDirName)
	q.prefix = "log"
	q.ext = ".json"
	q.label = "OTLP logs queue"
	q.unit = "records"
	q.fullReason = "logs_queue_full"
	q.ageReason = "dropped_by_age"
	q.maxBytes = maxBytes
	q.logger = log
	q.now = time.Now
	q.inspect = func(p string) (time.Time, int, error) {
		b, err := q.readBatchFile(p)
		return b.SavedAt, len(b.Records), err
	}
	q.onQueued = agentstate.IncrementOTLPLogsQueued
	q.onSize = agentstate.RecordOTLPLogsQueueSize
	q.recover()
	return q
}

// announce states once, at start, that failed log batches are kept on
// disk.
func (q *logsQueue) announce() {
	q.diskQueue.announce("OTLP logs queue active: failed log batches are kept on disk and replayed when the collector answers")
}

// enqueue persists one batch of records as a single file (atomic
// tmp+rename), then evicts oldest files if the size cap is exceeded.
func (q *logsQueue) enqueue(records []persistedLogRecord) error {
	if len(records) == 0 {
		return nil
	}
	batch := logBatch{Version: logsQueueFileVersion, SavedAt: q.now(), Records: records}
	data, err := json.Marshal(&batch)
	if err != nil {
		return fmt.Errorf("marshal log batch: %w", err)
	}
	return q.commit(data, len(records))
}

func (q *logsQueue) readBatchFile(path string) (logBatch, error) {
	var b logBatch
	data, err := os.ReadFile(path) // #nosec G304 - path built from queue dir
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, err
	}
	if b.Version > logsQueueFileVersion {
		return logBatch{}, fmt.Errorf("queue file version %d newer than supported %d", b.Version, logsQueueFileVersion)
	}
	return b, nil
}

// errBatchDiscarded is returned by a send function to say the batch will
// never be accepted (the receiver refused the payload on its merits): the
// file is removed without being counted as replayed.
var errBatchDiscarded = errors.New("batch discarded")

// drainAcked sends every queued batch in FIFO order and removes a file only
// once send returned nil, which is the backend's acknowledgement. The first
// failure stops the drain and leaves that file and every later one on disk.
// Returns the number of records acknowledged.
func (q *logsQueue) drainAcked(send func([]persistedLogRecord) error) (int, error) {
	replayed := 0
	for _, name := range q.files() {
		path := filepath.Join(q.dir, name)
		b, err := q.readBatchFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // evicted by the size cap or the age sweep meanwhile
			}
			// Corrupt or too-new file: drop it so it can't wedge the queue.
			q.removeFile(name)
			if q.logger != nil {
				q.logger.Warn().Err(err).Str("file", name).Msg("OTLP logs queue: dropping unreadable file")
			}
			continue
		}
		switch sendErr := send(b.Records); {
		case sendErr == nil:
			replayed += len(b.Records)
			agentstate.IncrementOTLPLogsReplayed(len(b.Records))
			q.removeFile(name)
		case errors.Is(sendErr, errBatchDiscarded):
			q.removeFile(name)
		default:
			// Part of the batch got through: only the rest stays queued,
			// or the next replay delivers those records a second time.
			var rem *replayRemainderError
			if errors.As(sendErr, &rem) {
				if len(rem.remaining) == 0 {
					q.removeFile(name)
				} else {
					q.rewriteFile(name, b, rem.remaining)
				}
			}
			return replayed, sendErr
		}
	}
	return replayed, nil
}

// replayRemainderError carries what a replayed batch still has to deliver.
type replayRemainderError struct {
	err       error
	remaining []persistedLogRecord
}

func (e *replayRemainderError) Error() string { return e.err.Error() }
func (e *replayRemainderError) Unwrap() error { return e.err }

// rewriteFile replaces a queued batch with the records it still has to
// deliver, keeping its name and SavedAt so FIFO order and the age limit
// are unchanged. On a write failure the old file stays: at-least-once.
func (q *logsQueue) rewriteFile(name string, old logBatch, remaining []persistedLogRecord) {
	batch := logBatch{Version: logsQueueFileVersion, SavedAt: old.SavedAt, Records: remaining}
	data, err := json.Marshal(&batch)
	if err != nil {
		if q.logger != nil {
			q.logger.Warn().Err(err).Str("file", name).Msg("OTLP logs queue: failed to marshal remainder batch")
		}
		return
	}
	q.rewrite(name, data, len(old.Records), len(remaining))
}

// persistentLogExporter decorates an sdklog.Exporter: on a failed
// export it persists the event-log records to the dead-letter queue.
// Whether the backend is answering is tracked by the embedded
// exporterHealth.
type persistentLogExporter struct {
	exporterHealth
	wrapped sdklog.Exporter
	queue   *logsQueue

	// reporter takes the consumer's partial rejections out of the export
	// error before it is classified. Without it a delivered batch the
	// consumer partly refused reads as a transport failure and lands on
	// disk, to be replayed against a consumer that already has it (#819).
	reporter *partialSuccessReporter

	// lostRecords counts event logs that could not be written to the queue.
	lostRecords atomic.Int64
}

func newPersistentLogExporter(wrapped sdklog.Exporter, queue *logsQueue, log *logger.ModuleLogger) *persistentLogExporter {
	e := &persistentLogExporter{
		wrapped:  wrapped,
		queue:    queue,
		reporter: newPartialSuccessReporter(log, "export"),
	}
	e.exporterHealth.init("otlp/logs", log)
	e.backlog = queue.hasFiles
	return e
}

// recordFailure counts a failed export and returns the classified error. A
// throttle is counted apart (noteThrottle): the backend is up and asked for
// less, which is not an export error.
func (e *persistentLogExporter) recordFailure(err error, throttled bool) error {
	// Count BEFORE persisting: a rejected batch that lands in the
	// dead-letter queue is still a failed export. In production a
	// receiver rejecting every logs batch with 400 never moved any
	// counter — the pipeline died silently behind queue/replay churn
	// (#820, tracked as #821).
	if !throttled {
		agentstate.IncrementOTLPExportErrors("logs")
	}
	classified := classifyExportError(err)
	agentstate.IncrementExportSendFailed("otlp", exporterrors.Reason(classified))
	agentstate.RecordExportFailure("otlp/logs", redactSensitive(err.Error()))
	return classified
}

func (e *persistentLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	started := e.now()
	if e.stopping.Load() {
		if !e.knownUp() {
			if !e.persist(records) {
				return errLogsBackendDown
			}
			return nil
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(e.drainTimeout.Load()))
		defer cancel()
	} else {
		if e.backendDown() && !e.claimAttempt() {
			if !e.persist(records) {
				return errLogsBackendDown
			}
			return nil
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-e.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	// A partial success is not a failed export: what came back with it is.
	// Counting the refused records here and dropping them from the error
	// is what keeps a delivered batch out of the dead-letter queue. A
	// split export says which of its records were not delivered: only
	// those are counted or queued.
	raw := e.wrapped.Export(ctx, records)
	var se *splitExportError
	split := errors.As(raw, &se)
	err := e.reporter.reportRejections(raw)
	if err != nil {
		throttled := exporterrors.IsRetryable(classifyExportError(err)) && e.noteThrottle(err)
		classified := e.recordFailure(err, throttled)
		e.markFailure()

		if split {
			if se.rejected > 0 {
				for i := 0; i < se.rejected; i++ {
					agentstate.IncrementOTLPDropped("receiver_rejected")
				}
				e.warnThrottled(func(ev *zerolog.Event) {
					ev.Str("error", redactSensitive(err.Error())).
						Int("records", se.rejected).
						Msg("OTLP logs export rejected by the receiver; records discarded (not queued — a retry gets the same rejection)")
				})
			}
			if len(se.undelivered) > 0 {
				e.persist(se.undelivered)
				if !throttled {
					e.warnThrottled(func(ev *zerolog.Event) {
						ev.Str("error", redactSensitive(err.Error())).
							Int("records", len(se.undelivered)).
							Msg("OTLP logs export failed; batch persisted to dead-letter queue")
					})
				}
			}
			return err
		}

		if !exporterrors.IsRetryable(classified) {
			// The receiver refused the payload on its merits. Persisting
			// it would put a batch on disk that is replayed at boot and
			// on every recovery, rejected every time, and re-persisted —
			// occupying space the drop-oldest eviction then takes from
			// batches that could still be delivered (#833).
			for i := 0; i < len(records); i++ {
				agentstate.IncrementOTLPDropped("receiver_rejected")
			}
			e.warnThrottled(func(ev *zerolog.Event) {
				ev.Str("error", redactSensitive(err.Error())).
					Int("records", len(records)).
					Msg("OTLP logs export rejected by the receiver; records discarded (not queued — a retry gets the same rejection)")
			})
			return err
		}

		e.persist(records)
		if !throttled {
			e.warnThrottled(func(ev *zerolog.Event) {
				ev.Str("error", redactSensitive(err.Error())).
					Int("records", len(records)).
					Msg("OTLP logs export failed; batch persisted to dead-letter queue")
			})
		}
		return err
	}
	e.markSuccessSince(started)
	return nil
}

// errLogsBackendDown is returned when a batch could not be exported (the
// backend is known down) and could not be written to disk either.
var errLogsBackendDown = errors.New("otlp logs backend down and the queue did not take the batch")

// exportDirect sends records to the backend and reports the outcome; the
// queue replay uses it, so a failure is never persisted a second time. A
// cancelled ctx (the agent is stopping) is not a verdict on the backend.
func (e *persistentLogExporter) exportDirect(ctx context.Context, records []sdklog.Record) error {
	started := e.now()
	raw := e.wrapped.Export(ctx, records)
	var se *splitExportError
	split := errors.As(raw, &se)
	err := e.reporter.reportRejections(raw)
	if err == nil {
		e.markReplaySuccess(started)
		return nil
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		throttled := exporterrors.IsRetryable(classifyExportError(err)) && e.noteThrottle(err)
		if classified := e.recordFailure(err, throttled); exporterrors.IsRetryable(classified) && (!split || len(se.undelivered) > 0) {
			e.markFailure()
		}
		if throttled && !split {
			err = &throttledError{err: err, wait: e.throttleRemaining()}
		}
	}
	if split {
		// The replay needs to know which records got through even when
		// it is being cancelled, or the delivered ones are sent again.
		return &splitExportError{err: err, undelivered: se.undelivered, rejected: se.rejected, dropped: se.dropped}
	}
	return err
}

// persist serialises the event-log records of a failed batch to the
// queue. Entity events are skipped (serializeEventLog returns ok=false).
// Returns false when event logs were left unwritten.
func (e *persistentLogExporter) persist(records []sdklog.Record) bool {
	batch := make([]persistedLogRecord, 0, len(records))
	for _, r := range records {
		if pr, ok := serializeEventLog(r); ok {
			batch = append(batch, pr)
		}
	}
	if len(batch) == 0 {
		return true
	}
	if err := e.queue.enqueue(batch); err != nil {
		e.warnThrottled(func(ev *zerolog.Event) {
			ev.Err(err).Int("records", len(batch)).Msg("OTLP logs queue: enqueue failed; records lost")
		})
		e.lostRecords.Add(int64(len(batch)))
		return false
	}
	e.fireQueued()
	return true
}

func (e *persistentLogExporter) ForceFlush(ctx context.Context) error {
	return e.wrapped.ForceFlush(ctx)
}

func (e *persistentLogExporter) Shutdown(ctx context.Context) error {
	return e.wrapped.Shutdown(ctx)
}

// logsReplayer drains the dead-letter queue to the backend, one batch at a
// time, deleting a file only after the export was acknowledged. Concurrent
// drains (boot replay, recovery, retry loop) collapse to one.
type logsReplayer struct {
	replayLoop
	queue         *logsQueue
	pipeline      *logsPipeline
	exporter      *persistentLogExporter
	running       atomic.Bool
	exportTimeout time.Duration
}

func newLogsReplayer(q *logsQueue, p *logsPipeline, exp *persistentLogExporter, log *logger.ModuleLogger) *logsReplayer {
	r := &logsReplayer{
		queue: q, pipeline: p, exporter: exp,
		exportTimeout: replayExportTimeout,
	}
	r.initLoop("OTLP logs queue", log)
	r.pending = q.pending
	r.run = r.replay
	r.sweep = func() { q.sweepAged() }
	r.afterStop = exp.closeRecovery
	r.hold = exp.throttleRemaining
	return r
}

// replay drains the queue once and returns the records acknowledged by the
// backend. On the first failed export it stops and returns the error: that
// file and the ones after it stay on disk. Concurrent calls collapse to one
// (the recovery callback and the boot replay can race).
func (r *logsReplayer) replay() (int, error) {
	if !r.running.CompareAndSwap(false, true) {
		return 0, nil
	}
	defer r.running.Store(false)

	r.queue.sweepAged()
	n, err := r.queue.drainAcked(r.sendBatch)
	if n > 0 && r.logger != nil {
		r.logger.Info().Int("records", n).Msg("OTLP logs queue: replayed records")
	}
	if err != nil && r.logger != nil && r.ctx.Err() == nil {
		if _, throttled := asThrottled(err); throttled {
			r.logger.Debug().Err(err).Int("replayed", n).
				Msg("OTLP logs queue: replay paused, the backend is throttling; remaining batches stay on disk")
		} else {
			r.logger.Warn().Err(err).Int("replayed", n).
				Msg("OTLP logs queue: replay stopped, the backend did not acknowledge; remaining batches stay on disk")
		}
	}
	return n, err
}

// sendBatch exports one queued batch and returns nil only on acknowledgement.
func (r *logsReplayer) sendBatch(prs []persistedLogRecord) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.exportTimeout)
	defer cancel()
	records := r.pipeline.rebuildEventLogs(ctx, prs)
	if len(records) == 0 {
		return nil
	}
	err := r.exporter.exportDirect(ctx, records)
	if err == nil {
		return nil
	}
	var se *splitExportError
	if errors.As(err, &se) {
		if se.rejected > 0 {
			for i := 0; i < se.rejected; i++ {
				agentstate.IncrementOTLPDropped("receiver_rejected")
			}
			if r.logger != nil {
				r.logger.Warn().Str("error", redactSensitive(err.Error())).Int("records", se.rejected).
					Msg("OTLP logs queue: the receiver refused a queued batch; discarded")
			}
		}
		if len(se.undelivered) == 0 {
			return errBatchDiscarded
		}
		remaining := make([]persistedLogRecord, 0, len(se.undelivered))
		for _, rec := range se.undelivered {
			pr, ok := serializeEventLog(rec)
			if !ok {
				continue
			}
			remaining = append(remaining, pr)
		}
		return &replayRemainderError{err: err, remaining: remaining}
	}
	if r.ctx.Err() == nil && !exporterrors.IsRetryable(classifyExportError(err)) {
		for range prs {
			agentstate.IncrementOTLPDropped("receiver_rejected")
		}
		if r.logger != nil {
			r.logger.Warn().Str("error", redactSensitive(err.Error())).Int("records", len(prs)).
				Msg("OTLP logs queue: the receiver refused a queued batch; discarded")
		}
		return errBatchDiscarded
	}
	return err
}

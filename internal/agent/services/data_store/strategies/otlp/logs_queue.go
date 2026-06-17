package otlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"senhub-agent.go/internal/agent/services/agentstate"
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
// Guarantee: at-least-once. A record may be exported twice if the
// backend received it but the ack was lost; OTLP consumers dedupe by
// (timestamp, body, attributes). The residual loss window is a hard
// crash while records sit in the SDK's in-memory batch (never handed to
// Export) — covered by the graceful-shutdown flush, not by a kill -9.

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
// Only string-valued bodies/attributes are represented — the only shape
// the event-log path produces (see logsPipeline.emit).
type persistedLogRecord struct {
	TimestampUnixNano         int64             `json:"ts"`
	ObservedTimestampUnixNano int64             `json:"obs_ts,omitempty"`
	SeverityNumber            int               `json:"sev"`
	SeverityText              string            `json:"sev_text,omitempty"`
	Body                      string            `json:"body"`
	Attributes                map[string]string `json:"attrs,omitempty"`
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
	r.WalkAttributes(func(kv log.KeyValue) bool {
		if out.Attributes == nil {
			out.Attributes = map[string]string{}
		}
		out.Attributes[string(kv.Key)] = kv.Value.AsString()
		return true
	})
	return out, true
}

// logsQueue is the on-disk dead-letter store: one JSON file per failed
// batch under <path>/logqueue/. Files are named monotonically so a
// lexical sort yields FIFO replay order.
type logsQueue struct {
	dir      string
	maxBytes int64
	logger   *logger.ModuleLogger

	mu        sync.Mutex
	seq       uint64 // monotonic file sequence
	sizeBytes int64
	records   int
}

func newLogsQueue(path string, maxBytes int64, log *logger.ModuleLogger) *logsQueue {
	if maxBytes <= 0 {
		maxBytes = defaultLogsQueueMaxBytes
	}
	q := &logsQueue{
		dir:      filepath.Join(path, logsQueueDirName),
		maxBytes: maxBytes,
		logger:   log,
	}
	q.recover()
	return q
}

// recover scans an existing queue directory at startup so size/seq
// counters reflect what is already on disk (a previous run's residue).
func (q *logsQueue) recover() {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return // missing dir = empty queue; created lazily on first enqueue
	}
	var total int64
	var recs int
	var maxSeq uint64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			continue
		}
		total += info.Size()
		var s uint64
		if _, scanErr := fmt.Sscanf(e.Name(), "log-%020d.json", &s); scanErr == nil && s > maxSeq {
			maxSeq = s
		}
		if b, readErr := q.readBatchFile(filepath.Join(q.dir, e.Name())); readErr == nil {
			recs += len(b.Records)
		}
	}
	q.sizeBytes = total
	q.records = recs
	q.seq = maxSeq
	agentstate.RecordOTLPLogsQueueSize(q.records, q.sizeBytes)
}

// enqueue persists one batch of records as a single file (atomic
// tmp+rename), then evicts oldest files if the size cap is exceeded.
func (q *logsQueue) enqueue(records []persistedLogRecord) error {
	if len(records) == 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := os.MkdirAll(q.dir, 0o750); err != nil {
		return fmt.Errorf("mkdir logqueue: %w", err)
	}
	q.seq++
	name := fmt.Sprintf("log-%020d.json", q.seq)
	batch := logBatch{Version: logsQueueFileVersion, SavedAt: time.Now(), Records: records}
	data, err := json.Marshal(&batch)
	if err != nil {
		return fmt.Errorf("marshal log batch: %w", err)
	}
	final := filepath.Join(q.dir, name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil { // #nosec G306
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	q.sizeBytes += int64(len(data))
	q.records += len(records)
	agentstate.IncrementOTLPLogsQueued(len(records))
	q.evictLocked()
	agentstate.RecordOTLPLogsQueueSize(q.records, q.sizeBytes)
	return nil
}

// evictLocked drops oldest files until under the byte cap. Caller holds mu.
func (q *logsQueue) evictLocked() {
	if q.sizeBytes <= q.maxBytes {
		return
	}
	files := q.listLocked()
	for _, name := range files {
		if q.sizeBytes <= q.maxBytes {
			break
		}
		path := filepath.Join(q.dir, name)
		b, err := q.readBatchFile(path)
		dropped := 0
		if err == nil {
			dropped = len(b.Records)
		}
		info, _ := os.Stat(path)
		if rmErr := os.Remove(path); rmErr != nil {
			continue
		}
		if info != nil {
			q.sizeBytes -= info.Size()
		}
		q.records -= dropped
		for i := 0; i < dropped; i++ {
			agentstate.IncrementOTLPDropped("logs_queue_full")
		}
		if q.logger != nil {
			q.logger.Warn().Str("file", name).Int("dropped", dropped).
				Msg("OTLP logs queue over size cap; evicted oldest batch")
		}
	}
	if q.sizeBytes < 0 {
		q.sizeBytes = 0
	}
	if q.records < 0 {
		q.records = 0
	}
}

// listLocked returns queue file names in FIFO (lexical = seq) order.
// Caller holds mu.
func (q *logsQueue) listLocked() []string {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
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

// drain replays every queued batch through emit, removing each file once
// its records have been re-emitted. emit hands records to the pipeline
// (async via the BatchProcessor); if the backend is still down the
// decorator re-persists them as a fresh file — so removing the source
// file right after emit never loses data. Returns the number of records
// replayed.
func (q *logsQueue) drain(emit func([]persistedLogRecord)) int {
	q.mu.Lock()
	files := q.listLocked()
	q.mu.Unlock()

	replayed := 0
	for _, name := range files {
		path := filepath.Join(q.dir, name)
		b, err := q.readBatchFile(path)
		if err != nil {
			// Corrupt or too-new file: drop it so it can't wedge the queue.
			q.removeFile(name)
			if q.logger != nil {
				q.logger.Warn().Err(err).Str("file", name).Msg("OTLP logs queue: dropping unreadable file")
			}
			continue
		}
		emit(b.Records)
		replayed += len(b.Records)
		agentstate.IncrementOTLPLogsReplayed(len(b.Records))
		q.removeFile(name)
	}
	return replayed
}

func (q *logsQueue) removeFile(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	path := filepath.Join(q.dir, name)
	info, _ := os.Stat(path)
	b, _ := q.readBatchFile(path)
	if err := os.Remove(path); err != nil {
		return
	}
	if info != nil {
		q.sizeBytes -= info.Size()
	}
	q.records -= len(b.Records)
	if q.sizeBytes < 0 {
		q.sizeBytes = 0
	}
	if q.records < 0 {
		q.records = 0
	}
	agentstate.RecordOTLPLogsQueueSize(q.records, q.sizeBytes)
}

// persistentLogExporter decorates an sdklog.Exporter: on a failed
// export it persists the event-log records to the dead-letter queue,
// and it fires onRecovered the first time an export succeeds after a
// failure (so the strategy can drain the queue).
type persistentLogExporter struct {
	wrapped sdklog.Exporter
	queue   *logsQueue
	logger  *logger.ModuleLogger

	healthy     atomic.Bool
	onRecovered atomic.Pointer[func()]
}

func newPersistentLogExporter(wrapped sdklog.Exporter, queue *logsQueue, log *logger.ModuleLogger) *persistentLogExporter {
	e := &persistentLogExporter{wrapped: wrapped, queue: queue, logger: log}
	e.healthy.Store(true)
	return e
}

func (e *persistentLogExporter) setOnRecovered(fn func()) {
	e.onRecovered.Store(&fn)
}

func (e *persistentLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	err := e.wrapped.Export(ctx, records)
	if err != nil {
		e.persist(records)
		e.healthy.Store(false)
		return err
	}
	// Export succeeded: if we were unhealthy, the backend just recovered.
	if e.healthy.CompareAndSwap(false, true) {
		if p := e.onRecovered.Load(); p != nil && *p != nil {
			go (*p)()
		}
	}
	return nil
}

// persist serialises the event-log records of a failed batch to the
// queue. Entity events are skipped (serializeEventLog returns ok=false).
func (e *persistentLogExporter) persist(records []sdklog.Record) {
	batch := make([]persistedLogRecord, 0, len(records))
	for _, r := range records {
		if pr, ok := serializeEventLog(r); ok {
			batch = append(batch, pr)
		}
	}
	if len(batch) == 0 {
		return
	}
	if err := e.queue.enqueue(batch); err != nil && e.logger != nil {
		e.logger.Warn().Err(err).Int("records", len(batch)).Msg("OTLP logs queue: enqueue failed; records lost")
	}
}

func (e *persistentLogExporter) ForceFlush(ctx context.Context) error {
	return e.wrapped.ForceFlush(ctx)
}

func (e *persistentLogExporter) Shutdown(ctx context.Context) error {
	return e.wrapped.Shutdown(ctx)
}

// logsReplayer drains the dead-letter queue back through the pipeline,
// guarding against concurrent drains (boot replay vs recovery replay).
type logsReplayer struct {
	queue    *logsQueue
	pipeline *logsPipeline
	logger   *logger.ModuleLogger
	running  atomic.Bool
}

func newLogsReplayer(q *logsQueue, p *logsPipeline, log *logger.ModuleLogger) *logsReplayer {
	return &logsReplayer{queue: q, pipeline: p, logger: log}
}

// replay drains the queue once. Concurrent calls collapse to one (the
// recovery callback and the boot replay can race).
func (r *logsReplayer) replay() {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)

	n := r.queue.drain(func(records []persistedLogRecord) {
		ctx := context.Background()
		for _, pr := range records {
			r.pipeline.replayEventLog(ctx, pr)
		}
	})
	if n > 0 && r.logger != nil {
		r.logger.Info().Int("records", n).Msg("OTLP logs queue: replayed records")
	}
}

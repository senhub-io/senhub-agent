package otlp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// diskQueue is the part of an on-disk dead-letter queue that does not depend
// on what a file holds: FIFO naming, size and age bounds, eviction, depth
// accounting, atomic writes. The logs queue keeps JSON batches in it, the
// metrics and traces queues keep serialized OTLP requests (wire_queue.go).
//
// A file is one unit of delivery. "units" is what the file carries (log
// records, data points, spans): the figure the depth gauges and the drop
// counter are expressed in, read from the file by inspect.
type diskQueue struct {
	dir string
	// prefix and ext build the file names: <prefix>-<seq 20 digits><ext>. A
	// lexical sort of the names is the FIFO order.
	prefix string
	ext    string
	// label opens every log line of the queue ("OTLP logs queue").
	label string
	// unit names what a file carries, for log lines.
	unit string
	// fullReason and ageReason label the senhub.agent.otlp.dropped counter
	// when the size cap or the retention takes data away.
	fullReason string
	ageReason  string

	maxBytes int64
	logger   *logger.ModuleLogger

	// maxAge drops batches that waited longer than this; 0 keeps them
	// until the byte cap evicts them. now is injectable for tests.
	maxAge time.Duration
	now    func() time.Time

	// inspect reads what the queue needs to know about one file without the
	// payload when the format allows it.
	inspect func(path string) (savedAt time.Time, units int, err error)
	// onQueued, onSize publish the cumulative and the depth figures.
	onQueued func(units int)
	onSize   func(units int, bytes int64)

	mu        sync.Mutex
	seq       uint64 // monotonic file sequence
	sizeBytes int64
	records   int // units on disk
	// oldestAt is when the batch that has waited longest was queued, so
	// how late the rail is running can be read without touching the disk.
	oldestAt time.Time
}

// pending reports how many units wait on disk and how long the oldest
// has waited. A zero duration means nothing is waiting.
func (q *diskQueue) pending() (int, time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.records == 0 || q.oldestAt.IsZero() {
		return q.records, 0
	}
	return q.records, time.Since(q.oldestAt)
}

// hasFiles reports whether anything waits on disk.
func (q *diskQueue) hasFiles() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.records > 0 || q.sizeBytes > 0
}

func (q *diskQueue) publishLocked() {
	if q.onSize != nil {
		q.onSize(q.records, q.sizeBytes)
	}
}

func (q *diskQueue) fileName(seq uint64) string {
	return fmt.Sprintf("%s-%020d%s", q.prefix, seq, q.ext)
}

// recover scans an existing queue directory at startup so size/seq
// counters reflect what is already on disk (a previous run's residue).
func (q *diskQueue) recover() {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return // missing dir = empty queue; created lazily on first enqueue
	}
	var total int64
	var recs int
	var maxSeq uint64
	format := q.prefix + "-%020d" + q.ext
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), q.ext+".tmp") {
			// A write that never reached its rename: no reader will ever
			// take it, and left alone it would sit outside the size cap.
			_ = os.Remove(filepath.Join(q.dir, e.Name()))
			continue
		}
		if filepath.Ext(e.Name()) != q.ext {
			continue
		}
		info, statErr := e.Info()
		if statErr != nil {
			continue
		}
		total += info.Size()
		var s uint64
		if _, scanErr := fmt.Sscanf(e.Name(), format, &s); scanErr == nil && s > maxSeq {
			maxSeq = s
		}
		if _, units, readErr := q.inspect(filepath.Join(q.dir, e.Name())); readErr == nil {
			recs += units
		}
	}
	q.sizeBytes = total
	q.records = recs
	// Whatever survived a restart has waited at least since now; the real
	// age is on the file, and it is only used to say how late the rail is.
	if recs > 0 {
		q.oldestAt = time.Now()
	}
	q.seq = maxSeq
	q.publishLocked()
}

// announce states once, at start, that failed batches are kept on disk. A
// queue that stays silent while healthy leaves an operator unable to tell it
// from a queue that is off.
func (q *diskQueue) announce(msg string) {
	q.mu.Lock()
	maxBytes, maxAge := q.maxBytes, q.maxAge
	q.mu.Unlock()
	ev := q.logger.Info().Str("path", q.dir).Int64("max_bytes", maxBytes)
	if maxAge > 0 {
		ev = ev.Dur("retention", maxAge)
	} else {
		ev = ev.Str("retention", "unlimited")
	}
	ev.Msg(msg)
}

// setMaxAge arms the age-based retention and sweeps once, so what a
// previous run left behind past the limit is gone before the boot replay.
func (q *diskQueue) setMaxAge(d time.Duration) {
	q.mu.Lock()
	q.maxAge = d
	q.mu.Unlock()
	q.sweepAged()
}

// sweepAged drops every batch saved longer ago than maxAge and reports
// the count once. Returns the number of units dropped.
func (q *diskQueue) sweepAged() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.maxAge <= 0 {
		return 0
	}
	cutoff := q.now().Add(-q.maxAge)
	droppedUnits, droppedFiles := 0, 0
	for _, name := range q.listLocked() {
		path := filepath.Join(q.dir, name)
		savedAt, units, err := q.inspect(path)
		if err != nil || !savedAt.Before(cutoff) {
			continue
		}
		info, _ := os.Stat(path)
		if rmErr := os.Remove(path); rmErr != nil {
			continue
		}
		if info != nil {
			q.sizeBytes -= info.Size()
		}
		q.records -= units
		droppedUnits += units
		droppedFiles++
		agentstate.AddOTLPDropped(q.ageReason, units)
	}
	if droppedFiles == 0 {
		return 0
	}
	if q.sizeBytes < 0 {
		q.sizeBytes = 0
	}
	if q.records <= 0 {
		q.records = 0
		q.oldestAt = time.Time{}
	}
	q.publishLocked()
	if q.logger != nil {
		q.logger.Warn().Int("dropped", droppedUnits).Int("batches", droppedFiles).
			Dur("max_age", q.maxAge).
			Msg(q.label + ": dropped " + q.unit + " older than the retention")
	}
	return droppedUnits
}

// prepareQueueDir creates dir and proves it accepts a write, so a state
// directory the service account cannot use is found at start rather
// than at the first outage.
func prepareQueueDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// commit writes one file (atomic tmp+rename) carrying units, then evicts
// the oldest files if the size cap is exceeded.
func (q *diskQueue) commit(data []byte, units int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := os.MkdirAll(q.dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Base(q.dir), err)
	}
	q.seq++
	final := filepath.Join(q.dir, q.fileName(q.seq))
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil { // #nosec G306
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	q.sizeBytes += int64(len(data))
	q.records += units
	if q.oldestAt.IsZero() {
		q.oldestAt = time.Now()
	}
	if q.onQueued != nil {
		q.onQueued(units)
	}
	q.evictLocked()
	q.publishLocked()
	return nil
}

// evictLocked drops oldest files until under the byte cap. Caller holds mu.
func (q *diskQueue) evictLocked() {
	if q.sizeBytes <= q.maxBytes {
		return
	}
	// The counters can be stale (a file removed behind our back, one that
	// could not be read when it left): confirm against the disk before
	// taking a live file for being over the cap.
	q.resyncLocked()
	if q.sizeBytes <= q.maxBytes {
		return
	}
	files := q.listLocked()
	for _, name := range files {
		if q.sizeBytes <= q.maxBytes {
			break
		}
		path := filepath.Join(q.dir, name)
		_, dropped, err := q.inspect(path)
		if err != nil {
			dropped = 0
		}
		info, _ := os.Stat(path)
		if rmErr := os.Remove(path); rmErr != nil {
			if !errors.Is(rmErr, os.ErrNotExist) {
				// Nothing more can be freed this way; the next commit
				// would only scan again.
				if q.logger != nil {
					q.logger.Warn().Err(rmErr).Str("file", name).Msg(q.label + " cannot evict the oldest batch")
				}
				break
			}
			// Gone already (removed behind our back): the counters are stale,
			// so they are read from the disk again.
			q.resyncLocked()
			continue
		}
		if info != nil {
			q.sizeBytes -= info.Size()
		}
		q.records -= dropped
		agentstate.AddOTLPDropped(q.fullReason, dropped)
		if q.logger != nil {
			q.logger.Warn().Str("file", name).Int("dropped", dropped).
				Msg(q.label + " over size cap; evicted oldest batch")
		}
	}
	if q.sizeBytes < 0 {
		q.sizeBytes = 0
	}
	if q.records < 0 {
		q.records = 0
	}
}

// resyncLocked recomputes the counters from the files on disk. Caller holds mu.
func (q *diskQueue) resyncLocked() {
	var size int64
	var units int
	for _, name := range q.listLocked() {
		path := filepath.Join(q.dir, name)
		if info, err := os.Stat(path); err == nil {
			size += info.Size()
		}
		if _, n, err := q.inspect(path); err == nil {
			units += n
		}
	}
	q.sizeBytes, q.records = size, units
	if units == 0 {
		q.oldestAt = time.Time{}
	}
}

// listLocked returns queue file names in FIFO (lexical = seq) order.
// Caller holds mu.
func (q *diskQueue) listLocked() []string {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == q.ext {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// files returns the queue file names in FIFO order.
func (q *diskQueue) files() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.listLocked()
}

func (q *diskQueue) removeFile(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	path := filepath.Join(q.dir, name)
	info, _ := os.Stat(path)
	_, units, _ := q.inspect(path)
	if err := os.Remove(path); err != nil {
		return
	}
	if info != nil {
		q.sizeBytes -= info.Size()
	}
	q.records -= units
	if q.sizeBytes < 0 {
		q.sizeBytes = 0
	}
	if q.records < 0 {
		q.records = 0
	}
	if len(q.listLocked()) == 0 {
		// Nothing left on disk: whatever the counters still say is drift,
		// from a file that could not be read when it left.
		q.records, q.sizeBytes = 0, 0
	}
	if q.records == 0 {
		q.oldestAt = time.Time{}
	}
	q.publishLocked()
}

// rewrite replaces a queued file with data, keeping its name so FIFO order
// is unchanged. On a write failure the old file stays: at-least-once.
func (q *diskQueue) rewrite(name string, data []byte, oldUnits, newUnits int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	path := filepath.Join(q.dir, name)
	oldSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		oldSize = info.Size()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil { // #nosec G306
		if q.logger != nil {
			q.logger.Warn().Err(err).Str("file", name).Msg(q.label + ": failed to write remainder file")
		}
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if q.logger != nil {
			q.logger.Warn().Err(err).Str("file", name).Msg(q.label + ": failed to rename remainder file")
		}
		return
	}
	q.sizeBytes += int64(len(data)) - oldSize
	q.records += newUnits - oldUnits
	if q.records < 0 {
		q.records = 0
	}
	if q.records == 0 {
		q.oldestAt = time.Time{}
	}
	q.publishLocked()
}

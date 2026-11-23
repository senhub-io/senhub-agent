package event

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
	eventtypes "senhub-agent.go/internal/agent/types/event"
)

const (
	backlogDirName = "event-queue"
	// backlogFileVersion stamps the on-disk batch shape. A reader that
	// meets a higher version leaves the file alone: a downgrade followed
	// by an upgrade must not destroy what the newer agent queued.
	backlogFileVersion = 1
	// DefaultBacklogMaxBytes caps the disk footprint of the backlog,
	// the same ceiling as the OTLP logs queue. An event is about 0.7 KB
	// on the wire, so 128 MiB holds roughly 190,000 events: four hours of
	// a host emitting 13 events per second, and far more than that for a
	// typical one.
	DefaultBacklogMaxBytes int64 = 128 << 20
	// DefaultBacklogMaxAge bounds how long an undelivered batch waits.
	// Six times the four hours an outage must be survived, short enough
	// that a replay after a long absence does not flood the intake with
	// events nobody is looking at any more.
	DefaultBacklogMaxAge = 24 * time.Hour
	// replayFilesPerSync bounds the batches replayed by one sync, so the
	// live buffer keeps being served while a large backlog drains.
	replayFilesPerSync = 20
	// maxReadFailures is how many replay rounds a file may fail to be
	// read (an I/O error, not a decoding one) before it is given up.
	maxReadFailures = 5
)

// backlogBatch is the on-disk envelope: one file per batch the intake did
// not acknowledge.
type backlogBatch struct {
	Version int                         `json:"version"`
	SavedAt time.Time                   `json:"saved_at"`
	Events  []eventtypes.EventDataPoint `json:"events"`
}

// backlogEntry is the in-memory index of one file, so that the size cap
// and the age limit are enforced without reading the disk.
type backlogEntry struct {
	name    string
	seq     uint64
	size    int64
	count   int
	savedAt time.Time
}

// eventBacklog is the durable retry queue of the event strategy: one JSON
// file per undelivered batch under dir, named by a monotonic sequence so
// that a sort yields the order the batches were queued in. A file leaves
// the disk only once the intake acknowledged exactly its content.
type eventBacklog struct {
	dir      string
	maxBytes int64
	maxAge   time.Duration
	logger   *logger.ModuleLogger
	now      func() time.Time
	// readFn reads a queue file; a field so a test can inject an I/O error.
	readFn func(string) ([]byte, error)

	// writeNanos is the total time append has spent writing files. A
	// stop is bounded by its deadline plus this: the events must reach
	// the disk to be kept, and a saturated disk can stall a write for
	// seconds. Tests read it to tell that cost from an overrun.
	writeNanos atomic.Int64

	mu sync.Mutex
	// readFailures counts, per file, the replay rounds that could not read it.
	readFailures map[uint64]int
	seq          uint64
	entries      []backlogEntry // ascending seq
	size         int64
}

// errBatchRefused is returned by a replay send function to say the intake
// refused the batch on its merits: the file is removed and counted dropped.
var errBatchRefused = errors.New("batch refused by the intake")

// prepareBacklogDir creates dir and proves it accepts a write, so a state
// directory the service account cannot use is found at start rather than
// at the first outage.
func prepareBacklogDir(dir string) error {
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

func newEventBacklog(dir string, maxBytes int64, maxAge time.Duration, log *logger.ModuleLogger) *eventBacklog {
	if maxBytes <= 0 {
		maxBytes = DefaultBacklogMaxBytes
	}
	if maxAge <= 0 {
		maxAge = DefaultBacklogMaxAge
	}
	b := &eventBacklog{dir: dir, maxBytes: maxBytes, maxAge: maxAge, logger: log, now: time.Now, readFn: os.ReadFile, readFailures: map[uint64]int{}}
	b.recover()
	return b
}

func backlogFileName(seq uint64) string { return fmt.Sprintf("evt-%020d.json", seq) }

// recover indexes what a previous run left behind. A file that does not
// parse is removed; a file of a newer version is left in place and ignored.
func (b *eventBacklog) recover() {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	unknown, corrupt := 0, 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var seq uint64
		if _, scanErr := fmt.Sscanf(e.Name(), "evt-%020d.json", &seq); scanErr != nil {
			continue
		}
		if seq > b.seq {
			b.seq = seq
		}
		batch, readErr := b.readFile(e.Name())
		switch {
		case readErr == nil:
			info, statErr := e.Info()
			if statErr != nil {
				continue
			}
			b.entries = append(b.entries, backlogEntry{
				name: e.Name(), seq: seq, size: info.Size(),
				count: len(batch.Events), savedAt: batch.SavedAt,
			})
			b.size += info.Size()
		case errors.Is(readErr, errBacklogNewerVersion):
			unknown++
		case errors.Is(readErr, errBacklogCorrupt):
			corrupt++
			_ = os.Remove(filepath.Join(b.dir, e.Name()))
		default:
			// An I/O error says nothing about the content: leave the file.
			b.logger.Warn().Err(readErr).Str("file", e.Name()).
				Msg("Event backlog: a file could not be read at start; left in place")
		}
	}
	sort.Slice(b.entries, func(i, j int) bool { return b.entries[i].seq < b.entries[j].seq })
	if unknown > 0 || corrupt > 0 {
		b.logger.Warn().Int("newer_version", unknown).Int("corrupt_removed", corrupt).
			Str("path", b.dir).
			Msg("Event backlog: skipped files that cannot be replayed")
	}
}

var (
	errBacklogNewerVersion = errors.New("backlog file version is newer than supported")
	// errBacklogIO marks a failure to read the file at all (descriptor
	// exhaustion, permissions, a lock): the content may be fine.
	errBacklogIO = errors.New("backlog file could not be read")
	// errBacklogCorrupt marks content that is not a valid batch.
	errBacklogCorrupt = errors.New("backlog file is corrupt")
)

func (b *eventBacklog) readFile(name string) (backlogBatch, error) {
	var batch backlogBatch
	data, err := b.readFn(filepath.Join(b.dir, name)) // #nosec G304 - name built from the index
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return batch, err
		}
		return batch, fmt.Errorf("%w: %w", errBacklogIO, err)
	}
	// UseNumber keeps integers exact: a float64 round trip would alter
	// values above 2^53 between the first send and the replay.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&batch); err != nil {
		return backlogBatch{}, fmt.Errorf("%w: %w", errBacklogCorrupt, err)
	}
	if batch.Version > backlogFileVersion {
		return backlogBatch{}, fmt.Errorf("%w: %d > %d", errBacklogNewerVersion, batch.Version, backlogFileVersion)
	}
	return batch, nil
}

// pending reports how many events wait on disk and how many bytes they use.
func (b *eventBacklog) pending() (int, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.entries {
		n += e.count
	}
	return n, b.size
}

func (b *eventBacklog) files() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// append persists one batch (tmp file, rename), then enforces the age limit
// and the size cap. durable adds an fsync before the rename: it protects
// against a power cut, and on a loaded disk it can take seconds, so the
// stop path, which has a deadline and whose process exit leaves the page
// cache intact, turns it off.
func (b *eventBacklog) append(events []eventtypes.EventDataPoint, durable bool) error {
	if len(events) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	writeStart := time.Now()
	defer func() { b.writeNanos.Add(int64(time.Since(writeStart))) }()

	if err := os.MkdirAll(b.dir, 0o750); err != nil {
		return fmt.Errorf("creating backlog directory: %w", err)
	}
	b.seq++
	name := backlogFileName(b.seq)
	saved := b.now()
	data, err := json.Marshal(&backlogBatch{Version: backlogFileVersion, SavedAt: saved, Events: events})
	if err != nil {
		return fmt.Errorf("marshaling backlog batch: %w", err)
	}
	final := filepath.Join(b.dir, name)
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640) // #nosec G304
	if err != nil {
		return fmt.Errorf("creating backlog file: %w", err)
	}
	_, werr := f.Write(data)
	if werr == nil && durable {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing backlog file: %w", werr)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publishing backlog file: %w", err)
	}
	b.entries = append(b.entries, backlogEntry{
		name: name, seq: b.seq, size: int64(len(data)), count: len(events), savedAt: saved,
	})
	b.size += int64(len(data))
	b.enforceLocked()
	return nil
}

// enforceLocked drops batches past the age limit, then the oldest ones
// until the size cap holds. One warning per call, however many files go.
func (b *eventBacklog) enforceLocked() {
	cutoff := b.now().Add(-b.maxAge)
	var agedEvents, sizedEvents, agedFiles, sizedFiles int
	for len(b.entries) > 0 {
		head := b.entries[0]
		byAge := head.savedAt.Before(cutoff)
		bySize := b.size > b.maxBytes && len(b.entries) > 1
		if !byAge && !bySize {
			break
		}
		if err := os.Remove(filepath.Join(b.dir, head.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			break
		}
		b.entries = b.entries[1:]
		b.size -= head.size
		if byAge {
			agedEvents += head.count
			agedFiles++
		} else {
			sizedEvents += head.count
			sizedFiles++
		}
	}
	if agedEvents+sizedEvents == 0 {
		return
	}
	agentstate.IncrementPushBufferDropped("event", agedEvents+sizedEvents)
	b.logger.Warn().
		Int("dropped_by_age", agedEvents).Int("batches_by_age", agedFiles).
		Int("dropped_by_size", sizedEvents).Int("batches_by_size", sizedFiles).
		Dur("max_age", b.maxAge).Int64("max_bytes", b.maxBytes).
		Msg("Event backlog over its limits; oldest events dropped")
}

// sweep applies the age limit and the size cap without queueing anything.
func (b *eventBacklog) sweep() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enforceLocked()
}

// drain replays queued batches in order, at most limit of them, and removes
// a file only once send returned nil, which is the intake's acknowledgement.
// The first failure stops the drain and leaves that file and the later ones
// on disk. It returns the number of events acknowledged.
func (b *eventBacklog) drain(ctx context.Context, limit int, send func(context.Context, []eventtypes.EventDataPoint) error) (int, error) {
	b.mu.Lock()
	snapshot := append([]backlogEntry(nil), b.entries...)
	b.mu.Unlock()

	sent := 0
	for i, e := range snapshot {
		if i >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		batch, err := b.readFile(e.name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // evicted meanwhile
			}
			switch {
			case errors.Is(err, errBacklogCorrupt):
				b.logger.Warn().Err(err).Str("file", e.name).
					Msg("Event backlog: removing a corrupt file")
				b.remove(e.seq, false)
			case errors.Is(err, errBacklogIO):
				b.noteReadFailure(e)
			}
			continue
		}
		b.mu.Lock()
		delete(b.readFailures, e.seq)
		b.mu.Unlock()
		switch sendErr := send(ctx, batch.Events); {
		case sendErr == nil:
			sent += len(batch.Events)
			b.remove(e.seq, false)
		case errors.Is(sendErr, errBatchRefused):
			b.remove(e.seq, true)
		default:
			return sent, sendErr
		}
	}
	return sent, nil
}

// noteReadFailure keeps a file that could not be read this round, and
// gives it up, counted and logged once, after maxReadFailures rounds.
func (b *eventBacklog) noteReadFailure(e backlogEntry) {
	b.mu.Lock()
	b.readFailures[e.seq]++
	n := b.readFailures[e.seq]
	b.mu.Unlock()
	if n < maxReadFailures {
		return
	}
	b.logger.Warn().Str("file", e.name).Int("events", e.count).Int("rounds", n).
		Msg("Event backlog: giving up on a file that cannot be read; its events are dropped")
	b.remove(e.seq, true)
}

// remove deletes the file of seq from disk and index; countDropped records
// its events as lost.
func (b *eventBacklog) remove(seq uint64, countDropped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, e := range b.entries {
		if e.seq != seq {
			continue
		}
		if err := os.Remove(filepath.Join(b.dir, e.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.logger.Warn().Err(err).Str("file", e.name).Msg("Event backlog: could not remove a delivered file")
			return
		}
		b.entries = append(b.entries[:i], b.entries[i+1:]...)
		delete(b.readFailures, seq)
		b.size -= e.size
		if countDropped {
			agentstate.IncrementPushBufferDropped("event", e.count)
		}
		return
	}
}

package otlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// Checkpoint format version. Bump when the on-disk schema changes in
// a way an older reader cannot ignore. A reader that sees a higher
// version than it knows refuses to restore (logs a warning, starts
// with an empty store) — better than partially restoring stale data
// in the wrong shape.
const checkpointFormatVersion = 1

// snapshotFileName is the canonical name written by save(). The
// matching .tmp variant is used during the atomic write dance.
const (
	snapshotFileName = "snapshot.json"
	snapshotTmpName  = "snapshot.json.tmp"
)

// checkpointConfig collects the operator-tunable knobs.
type checkpointConfig struct {
	Path     string        // directory; created if missing
	Interval time.Duration // save cadence
}

// checkpoint is the on-disk envelope. Version-stamped so future
// readers can route by format. The Saved field is purely informational
// (operator-friendly debugging — "when did this checkpoint happen?").
type checkpoint struct {
	Version int            `json:"version"`
	SavedAt time.Time      `json:"saved_at"`
	Entries []entrySnapshot `json:"entries"`
}

// entrySnapshot is the JSON-shaped projection of a storedMetric.
// We project rather than tagging the struct itself because storedMetric
// uses lowercase fields by design (package-private state), and adding
// JSON tags would force exposing those fields beyond the package.
type entrySnapshot struct {
	ProbeName  string            `json:"probe_name"`
	ProbeType  string            `json:"probe_type"`
	MetricName string            `json:"metric_name"`
	Value      float64           `json:"value"`
	Unit       string            `json:"unit,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	ObservedAt time.Time         `json:"observed_at"`
}

// checkpointer periodically writes the metric store's LWW snapshot to
// disk and restores it at boot. Scoped to the OTLP strategy because
// that's where the unbounded-cardinality risk and the restart-gap pain
// live; the HTTP strategy keeps its own cache with TTL eviction and
// is naturally rebuildable from probes at next cycle.
//
// Atomicity: every save writes to snapshot.json.tmp, fsyncs, then
// renames to snapshot.json. On Linux/Unix this rename is atomic;
// crashes mid-write leave the .tmp behind which loadAndRestore
// silently ignores. Hardware power-loss can leave a partial .tmp
// — same outcome, we ignore it.
//
// Lifecycle: same shape as memoryLimiter. start() launches the
// goroutine; stop() signals + waits; stop() is safe to call when
// start() was never called (guard via `started`). Calling stop()
// also performs one final synchronous save so a graceful shutdown
// preserves as much state as possible.
type checkpointer struct {
	cfg    checkpointConfig
	store  *metricStore
	logger *logger.ModuleLogger

	mu        sync.Mutex
	started   bool
	stopOnce  bool
	stopCh    chan struct{}
	stoppedCh chan struct{}

	lastSaveNanos atomic.Int64 // unix-nanos of the most recent successful save
	lastSizeBytes atomic.Int64 // size of the most recent saved file
}

// newCheckpointer creates a configured checkpointer. The constructor
// does NOT touch the filesystem — the directory is created on the
// first save (or by loadAndRestore if invoked first). This keeps the
// constructor cheap and side-effect-free, matching the rest of the
// package.
func newCheckpointer(cfg checkpointConfig, store *metricStore, log *logger.ModuleLogger) *checkpointer {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	return &checkpointer{
		cfg:       cfg,
		store:     store,
		logger:    log,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// enabled is true when a checkpoint path is configured. A zero-config
// checkpointer is treated as disabled — saves and restores become
// no-ops, no goroutine starts.
func (c *checkpointer) enabled() bool {
	return c != nil && c.cfg.Path != ""
}

// loadAndRestore reads the snapshot file (if any) and replays its
// entries into the store. Returns the number of entries restored and
// any error encountered. A missing file is NOT an error — the agent
// is allowed to boot with an empty store.
//
// Corrupt files (bad JSON, unknown version) log a warning, increment
// the checkpoint error counter, and are silently skipped. Better than
// refusing to boot — the operator sees the warning and the agent
// continues with a fresh store.
func (c *checkpointer) loadAndRestore() (int, error) {
	if !c.enabled() {
		return 0, nil
	}

	path := filepath.Join(c.cfg.Path, snapshotFileName)
	data, err := os.ReadFile(path) // #nosec G304 - path under operator-configured dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		agentstate.IncrementOTLPCheckpointErrors("read")
		return 0, fmt.Errorf("read checkpoint: %w", err)
	}

	var cp checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		agentstate.IncrementOTLPCheckpointErrors("parse")
		if c.logger != nil {
			c.logger.Warn().Err(err).Str("path", path).
				Msg("OTLP checkpoint file unreadable; starting with empty store")
		}
		return 0, nil
	}

	if cp.Version != checkpointFormatVersion {
		agentstate.IncrementOTLPCheckpointErrors("version_mismatch")
		if c.logger != nil {
			c.logger.Warn().
				Int("disk_version", cp.Version).
				Int("supported_version", checkpointFormatVersion).
				Str("path", path).
				Msg("OTLP checkpoint version mismatch; starting with empty store")
		}
		return 0, nil
	}

	c.store.restoreFromSnapshot(cp.Entries)
	agentstate.RecordOTLPCheckpointRestored(len(cp.Entries))
	return len(cp.Entries), nil
}

// save writes the current store state to disk atomically (tmp + rename).
// Safe to call from any goroutine; the underlying os ops are
// sufficient for the single-writer pattern used here.
func (c *checkpointer) save() error {
	if !c.enabled() {
		return nil
	}

	// Ensure the directory exists. Cheap and idempotent — the OS turns
	// a "directory already exists" into a nil error when MkdirAll is
	// called.
	if err := os.MkdirAll(c.cfg.Path, 0o750); err != nil {
		agentstate.IncrementOTLPCheckpointErrors("mkdir")
		return fmt.Errorf("mkdir checkpoint dir: %w", err)
	}

	entries := c.store.snapshotForCheckpoint()
	cp := checkpoint{
		Version: checkpointFormatVersion,
		SavedAt: time.Now(),
		Entries: entries,
	}

	tmpPath := filepath.Join(c.cfg.Path, snapshotTmpName)
	finalPath := filepath.Join(c.cfg.Path, snapshotFileName)

	// Write the tmp file, fsync, close, then atomic rename. The order
	// matters: rename without fsync risks losing the data if the OS
	// crashes between rename and durable disk write.
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640) // #nosec G304
	if err != nil {
		agentstate.IncrementOTLPCheckpointErrors("create_tmp")
		return fmt.Errorf("create tmp: %w", err)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(&cp); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		agentstate.IncrementOTLPCheckpointErrors("encode")
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		agentstate.IncrementOTLPCheckpointErrors("fsync")
		return fmt.Errorf("fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		agentstate.IncrementOTLPCheckpointErrors("close")
		return fmt.Errorf("close: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		agentstate.IncrementOTLPCheckpointErrors("rename")
		return fmt.Errorf("rename: %w", err)
	}

	info, statErr := os.Stat(finalPath)
	if statErr == nil {
		c.lastSizeBytes.Store(info.Size())
		agentstate.RecordOTLPCheckpointSize(info.Size())
	}
	now := time.Now().UnixNano()
	c.lastSaveNanos.Store(now)
	agentstate.RecordOTLPCheckpointLastSave(now)
	return nil
}

// start launches the periodic save goroutine. Safe to call once; a
// second start is a no-op. start does NOT save immediately — the
// first save fires after one Interval has elapsed. Operators wanting
// a startup save should call save() explicitly before start.
func (c *checkpointer) start(ctx context.Context) {
	if !c.enabled() {
		return
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()

	go c.runLoop(ctx)
}

// stop signals the goroutine to exit, performs a final synchronous
// save (best effort), and waits for the goroutine to fully exit.
// Idempotent and safe to call when start() was never called.
func (c *checkpointer) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopOnce {
		c.mu.Unlock()
		return
	}
	c.stopOnce = true
	wasStarted := c.started
	c.mu.Unlock()

	close(c.stopCh)
	if wasStarted {
		<-c.stoppedCh
	}

	// Final save so a graceful shutdown captures the latest state.
	// Errors logged but not propagated — we're already on the
	// shutdown path.
	if c.enabled() {
		if err := c.save(); err != nil && c.logger != nil {
			c.logger.Warn().Err(err).Msg("OTLP checkpoint final save failed")
		}
	}
}

// runLoop is the body of the periodic-save goroutine. Honors the
// ctx for cooperative cancellation (Strategy.Shutdown passes its
// shutdown context here).
func (c *checkpointer) runLoop(ctx context.Context) {
	defer close(c.stoppedCh)

	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			if err := c.save(); err != nil && c.logger != nil {
				c.logger.Warn().Err(err).Msg("OTLP checkpoint save failed")
			}
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

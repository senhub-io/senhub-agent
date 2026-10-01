// Package filetail tails one or more log files (or globs) and publishes
// each parsed line to the agent's log channel (agentstate.PublishLog)
// as an OTel-shaped LogRecord. It is the generic, cross-platform
// counterpart to linux_logs (which is systemd-journal-only): the use
// cases are flat-file application logs, Citrix VDA / Workspace logs,
// FSLogix / Profile Management logs, IIS logs, etc.
//
// Design mirrors linux_logs: an event-driven probe whose Collect() is a
// no-op. Per-file tail goroutines (backed by github.com/nxadm/tail,
// which handles rotation and reopen) push records onto the channel as
// lines arrive; the OTLP strategy consumes from there.
//
// Cross-platform: the package has no build tags. nxadm/tail opens files
// in shared-read mode, which is the non-exclusive access Windows
// requires to read an actively-written log.
package filetail

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nxadm/tail"

	"senhub-agent.go/internal/agent/probes/logparse"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// bookmarkFlushInterval bounds how often a per-file offset is persisted.
// Persisting on every line would thrash the disk on a busy log; on
// restart we may re-read up to this window of lines (at-least-once
// delivery), which is the standard tail-with-bookmark tradeoff.
const bookmarkFlushInterval = 2 * time.Second

// globRescanInterval controls how often path globs are re-expanded so
// files that appear after start (a new daily log, a newly-created VDA
// log) get picked up without restarting the probe.
const globRescanInterval = 15 * time.Second

// FileTailProbe is the generic file tail probe. Event-driven: Collect
// returns nil and the tail goroutines do the work.
type FileTailProbe struct {
	*types.BaseProbe
	config       FileTailProbeConfig
	moduleLogger *logger.ModuleLogger

	bookmarks bookmarkStore

	mu      sync.Mutex
	tailing map[string]*tail.Tail // active tails keyed by absolute path
	// awaiting holds literal paths that did not exist when first scanned.
	// When one appears, everything in it was written after the probe
	// started watching, so it is read from its first byte.
	awaiting map[string]bool
	wg       sync.WaitGroup
	quit     chan struct{}
	stopped  bool

	// emitted counts log records this probe instance has published to the
	// log rail — the conduit's own throughput self-metric, surfaced through
	// Collect so it flows to the metric sinks like any other probe (#701).
	emitted atomic.Uint64
}

// NewFileTailProbe constructs the probe. Validation of paths and the
// parser/multiline blocks happens here so a bad regex surfaces at config
// load, not silently at runtime.
func NewFileTailProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.filetail")

	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &FileTailProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       parsed,
		moduleLogger: moduleLogger,
		tailing:      map[string]*tail.Tail{},
		awaiting:     map[string]bool{},
		quit:         make(chan struct{}),
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

// GetTargetStrategies is intentionally NOT overridden: the tailed log
// records ride the log rail (agentstate.PublishLog), but Collect() also
// emits the conduit's throughput self-metric, which must route to the
// metric sinks like any other probe. It inherits the BaseProbe default
// (senhub, prtg, http, otlp). (Before #701 this returned []string{}, so
// any datapoint it emitted would have been dropped to no sink.)

// ShouldStart always returns true; path resolution happens in OnStart.
func (p *FileTailProbe) ShouldStart() bool { return true }

// GetInterval is irrelevant for an event-driven probe but the poller
// requires a value.
// GetInterval is the self-metric refresh cadence. Kept BELOW the HTTP
// cache TTL (default 5m) so the records_emitted series is refreshed well
// before it can reach the pull-cache eviction boundary (audit m8).
func (p *FileTailProbe) GetInterval() time.Duration { return 1 * time.Minute }

// Collect surfaces the conduit's own throughput self-metric: the tail
// goroutines publish log records directly to the log rail, so the only
// datapoint here is the cumulative count of records emitted (#701).
func (p *FileTailProbe) Collect() ([]data_store.DataPoint, error) {
	points := []data_store.DataPoint{
		{Name: "senhub.filetail.records_emitted", Value: float64(p.emitted.Load()), Timestamp: time.Now()},
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// OnStart loads the bookmark store, performs the first glob expansion,
// and launches a rescan loop that picks up new files over time.
func (p *FileTailProbe) OnStart(quitChannel chan struct{}) error {
	bm, err := newBookmark(p.config.BookmarkPath)
	if err != nil {
		return fmt.Errorf("filetail: bookmark init: %w", err)
	}
	p.bookmarks = bm

	p.moduleLogger.Info().
		Strs("paths", p.config.Paths).
		Str("parser", string(p.config.Parser.Type)).
		Str("bookmark_path", p.config.BookmarkPath).
		Bool("from_beginning", p.config.FromBeginning).
		Msg("Starting filetail probe")

	p.scanAndTail()

	p.wg.Add(1)
	go p.rescanLoop()

	go func() {
		select {
		case <-quitChannel:
		case <-p.quit:
		}
		p.shutdown(context.Background())
	}()

	return nil
}

// OnShutdown stops every active tail and the rescan loop, honoring the
// supplied deadline.
func (p *FileTailProbe) OnShutdown(ctx context.Context) error {
	p.shutdown(ctx)
	return nil
}

func (p *FileTailProbe) shutdown(ctx context.Context) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	close(p.quit)
	tails := make([]*tail.Tail, 0, len(p.tailing))
	for _, t := range p.tailing {
		tails = append(tails, t)
	}
	p.tailing = map[string]*tail.Tail{}
	p.mu.Unlock()

	for _, t := range tails {
		_ = t.Stop()
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		p.moduleLogger.Warn().Msg("filetail shutdown deadline elapsed before all tails drained")
	}
}

func (p *FileTailProbe) rescanLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(globRescanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.quit:
			return
		case <-ticker.C:
			p.scanAndTail()
		}
	}
}

// scanAndTail expands every configured path glob and starts a tail for
// any matched file not already being tailed.
func (p *FileTailProbe) scanAndTail() {
	for _, pattern := range p.config.Paths {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("pattern", pattern).Msg("invalid glob pattern; skipping")
			continue
		}
		// A literal path that does not exist yet is not tailed: a tail
		// opened on a missing file waits on an inotify watch of the parent
		// directory, which dies when that directory is missing and never
		// fires when a mount appears over it. The rescan picks the file up
		// once it exists.
		if len(matches) == 0 && !hasGlobMeta(pattern) {
			abs, err := filepath.Abs(pattern)
			if err != nil {
				abs = pattern
			}
			p.mu.Lock()
			p.awaiting[abs] = true
			p.mu.Unlock()
			continue
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil {
				abs = m
			}
			p.startTail(abs)
		}
	}
}

func (p *FileTailProbe) startTail(file string) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	if _, exists := p.tailing[file]; exists {
		p.mu.Unlock()
		return
	}

	fp := fingerprint(file, DefaultFingerprintLength)
	var size int64
	if fi, err := os.Stat(file); err == nil {
		size = fi.Size()
	}
	stored, hasStored := p.bookmarks.Get(file)
	offset := resolveStartOffset(stored, hasStored, fp, size, p.config.FromBeginning || p.awaiting[file])
	delete(p.awaiting, file)

	reopened := make(chan struct{})
	cfg := tail.Config{
		ReOpen:        true,
		Follow:        true,
		MustExist:     false,
		CompleteLines: true,
		Logger:        log.New(&reopenSignal{reopened: reopened, quit: p.quit}, "", 0),
		// On Windows a change notification for a file is not delivered
		// while its writer keeps it open: a log such as PRTG's, held open
		// for the life of the service, was never read, without an error
		// (#945). The size is polled there instead.
		Poll: runtime.GOOS == "windows",
	}
	// Tailing from the end seeks to the size just measured rather than to
	// the end at open time, so the offset recorded below is exactly where
	// reading starts.
	if offset < 0 {
		offset = size
	}
	cfg.Location = &tail.SeekInfo{Offset: offset, Whence: 0} // io.SeekStart

	t, err := tail.TailFile(file, cfg)
	if err != nil {
		p.mu.Unlock()
		p.moduleLogger.Warn().Err(err).Str("file", file).Msg("failed to start tail")
		return
	}
	p.tailing[file] = t
	p.wg.Add(1)
	p.mu.Unlock()

	// A file that produces no line during the run must still be
	// bookmarked: without an entry, or with the zero offset consume
	// would otherwise persist at stop, the next start reads it again.
	if err := p.bookmarks.Set(file, bookmarkEntry{Offset: offset, Fingerprint: fp}); err != nil {
		p.moduleLogger.Warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
	}

	go p.consume(file, t, offset, fp, reopened)
}

// reopenSignal turns nxadm/tail's log line announcing a reopen into an
// event. The library reports a rotated or truncated file only through its
// logger, and reads the new file from its first byte without a line to
// say so. The write happens on the tail's own goroutine, which has already
// handed over every line of the previous file (Lines is unbuffered) and
// sends none of the new one until the event is taken: consume sees the
// reopen exactly between the two files.
type reopenSignal struct {
	reopened chan<- struct{}
	quit     <-chan struct{}
}

func (r *reopenSignal) Write(b []byte) (int, error) {
	if bytes.HasPrefix(b, []byte("Successfully reopened")) {
		select {
		case r.reopened <- struct{}{}:
		case <-r.quit:
		}
	}
	return len(b), nil
}

// consume drains one file's tail channel, folds multiline records,
// parses each, publishes it, and periodically persists the offset.
//
// fp is the fingerprint of the file the offset was read from. It is only
// replaced when the tail reports a reopen, never recomputed from the path
// at persist time: by then the path may name a newer file, and pairing the
// old file's offset with the new file's head hash is what let a restart
// resume past the end of the file that replaced it.
func (p *FileTailProbe) consume(file string, t *tail.Tail, startOffset int64, fp string, reopened <-chan struct{}) {
	defer p.wg.Done()

	asm := logparse.NewAssembler(p.config.Multiline, p.config.MaxBytesPerLine)
	probeName := p.GetName()

	lastFlush := time.Now()
	lastOffset := startOffset

	persist := func() {
		// A file below the fingerprint window has none yet. Once the offset
		// shows the window was read, take it, provided the path still holds
		// at least that many bytes.
		if fp == "" && lastOffset >= DefaultFingerprintLength {
			if fi, err := os.Stat(file); err == nil && fi.Size() >= lastOffset {
				fp = fingerprint(file, DefaultFingerprintLength)
			}
		}
		if err := p.bookmarks.Set(file, bookmarkEntry{Offset: lastOffset, Fingerprint: fp}); err != nil {
			p.moduleLogger.Warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
		}
	}

	// A burst of lines inside one flush interval used to leave the
	// bookmark at its first line until the next line arrived: a crash
	// meanwhile replayed lines already sent. The ticker persists an
	// offset that moved, whether or not another line follows.
	ticker := time.NewTicker(bookmarkFlushInterval)
	defer ticker.Stop()
	dirty := false
read:
	for {
		var line *tail.Line
		select {
		case <-reopened:
			// The file was rotated or truncated and the tail now reads the
			// new one from its start. Bookmark that at once: until the next
			// line the entry held the previous file's offset, which the
			// periodic flush then paired with the new file's fingerprint,
			// and a restart in between resumed the new file at the old
			// offset, skipping or replaying its lines (#999).
			for _, logical := range asm.Flush() {
				p.publish(p.config.Parser, logical, time.Now(), probeName, file)
			}
			lastOffset = 0
			fp = fingerprint(file, DefaultFingerprintLength)
			persist()
			lastFlush = time.Now()
			dirty = false
			continue
		case <-ticker.C:
			if dirty {
				persist()
				lastFlush = time.Now()
				dirty = false
			}
			continue
		case l, ok := <-t.Lines:
			if !ok {
				break read
			}
			line = l
		}
		if line == nil {
			continue
		}
		if line.Err != nil {
			p.moduleLogger.Debug().Err(line.Err).Str("file", file).Msg("tail line error")
			continue
		}
		lastOffset = line.SeekInfo.Offset
		dirty = true

		readTime := line.Time
		if readTime.IsZero() {
			readTime = time.Now()
		}

		// nxadm/tail splits on "\n" and keeps a trailing "\r" on Windows
		// CRLF files; strip it so bodies/attributes are clean and parsers
		// behave identically across platforms.
		text := strings.TrimSuffix(line.Text, "\r")
		for _, logical := range asm.Append(text) {
			p.publish(p.config.Parser, logical, readTime, probeName, file)
		}

		if time.Since(lastFlush) >= bookmarkFlushInterval {
			persist()
			lastFlush = time.Now()
			dirty = false
		}
	}

	// Channel closed (tail stopped): flush any pending multiline record
	// and persist the final offset.
	for _, logical := range asm.Flush() {
		p.publish(p.config.Parser, logical, time.Now(), probeName, file)
	}
	persist()
	p.forgetDeadTail(file, t, t.Wait())
}

// forgetDeadTail drops a tail that ended on its own (the file vanished
// with its directory, a read error) so the next rescan starts a new one.
// Without it the dead tail stays registered and the file is never read
// again until the probe restarts.
func (p *FileTailProbe) forgetDeadTail(file string, t *tail.Tail, cause error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.tailing[file] != t {
		return
	}
	delete(p.tailing, file)
	p.moduleLogger.Warn().Err(cause).Str("file", file).Msg("tail ended; retrying on next rescan")
}

func (p *FileTailProbe) publish(pc ParserConfig, line string, readTime time.Time, probeName, file string) {
	rec, ok := logparse.ParseLine(pc, line, readTime, probeName, ProbeType)
	if !ok {
		p.moduleLogger.Debug().Str("file", file).Str("line", logparse.Truncate(line, 200)).
			Msg("line did not parse as declared json; skipping")
		return
	}
	if file != "" {
		rec.Attributes["log.file.path"] = file
	}
	rec.TargetStrategies = p.LogTargets()
	agentstate.PublishLog(rec)
	p.emitted.Add(1)
}

// hasGlobMeta reports whether a path pattern contains glob
// metacharacters. A literal path with none is treated as a single file
// to (re)open even before it exists.
func hasGlobMeta(p string) bool {
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// String formats the probe for log statements.
func (p *FileTailProbe) String() string {
	return fmt.Sprintf("FileTailProbe{paths=%v, parser=%s}", p.config.Paths, p.config.Parser.Type)
}

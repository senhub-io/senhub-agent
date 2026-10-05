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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nxadm/tail"
	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/probes/logparse"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
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
	tailing map[string]*tailState // active tails keyed by absolute path
	// awaiting holds literal paths that did not exist when first scanned.
	// When one appears, everything in it was written after the probe
	// started watching, so it is read from its first byte.
	awaiting map[string]bool
	// issues holds, per configured path or discovered file, why it cannot
	// be read right now. It is rebuilt by every scan and surfaced as a
	// Collect error, so a path the service cannot open does not look healthy.
	issues map[string]string
	// polling lists files whose tail was restarted after a stall: they are
	// followed by polling, which does not go through the inotify tracker
	// that every tail of the process shares.
	polling    map[string]bool
	stallGrace time.Duration
	wg         sync.WaitGroup
	quit       chan struct{}
	stopped    bool

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
		tailing:      map[string]*tailState{},
		polling:      map[string]bool{},
		stallGrace:   2 * globRescanInterval,
		awaiting:     map[string]bool{},
		issues:       map[string]string{},
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

// Collect surfaces the conduit's own self-metrics: the cumulative count of
// records emitted (#701) and, for every file currently tailed, how far the
// tail has read and how large the file is now. An offset below the size
// that does not close is a tail that stopped following its file, which the
// line count cannot show. The points are returned together with the
// unreadable-paths error, so a bad path does not hide the healthy ones.
func (p *FileTailProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points := []data_store.DataPoint{
		{Name: "senhub.filetail.records_emitted", Value: float64(p.emitted.Load()), Timestamp: now},
	}
	points = append(points, p.tailPositionPoints(now)...)
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), p.unreadableError()
}

func (p *FileTailProbe) tailPositionPoints(now time.Time) []data_store.DataPoint {
	type position struct {
		path   string
		offset int64
	}
	p.mu.Lock()
	positions := make([]position, 0, len(p.tailing))
	for path, ts := range p.tailing {
		positions = append(positions, position{path: path, offset: ts.offset.Load()})
	}
	p.mu.Unlock()
	sort.Slice(positions, func(i, j int) bool { return positions[i].path < positions[j].path })

	points := make([]data_store.DataPoint, 0, 2*len(positions))
	for _, pos := range positions {
		pathTags := []tags.Tag{{Key: "log.file.path", Value: pos.path}}
		points = append(points, data_store.DataPoint{
			Name: "senhub.filetail.read_offset", Value: float64(pos.offset), Timestamp: now, Tags: pathTags,
		})
		fi, err := os.Stat(pos.path)
		if err != nil {
			p.debug().Str("path", pos.path).Err(err).Msg("cannot stat tailed file, size not reported")
			continue
		}
		points = append(points, data_store.DataPoint{
			Name: "senhub.filetail.file_size", Value: float64(fi.Size()), Timestamp: now, Tags: pathTags,
		})
	}
	return points
}

// unreadableError reports the paths the last scan could not read, or nil.
func (p *FileTailProbe) unreadableError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A stalled tail the probe restarted is not listed: the file is
	// readable and read again, and failing the cycle reported a probe in
	// error while it collected. The restart is logged at Warn.
	all := make(map[string]string, len(p.issues))
	for k, v := range p.issues {
		all[k] = v
	}
	if len(all) == 0 {
		return nil
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+all[k])
	}
	return fmt.Errorf("filetail: %d configured path(s) cannot be read: %s", len(keys), strings.Join(parts, "; "))
}

// recordIssues replaces the unreadable-path set with the result of a scan.
// A line is logged only when a path's state changes, so a path that stays
// unreadable costs one line, not one per rescan.
func (p *FileTailProbe) recordIssues(current map[string]string) {
	p.mu.Lock()
	previous := p.issues
	p.issues = current
	p.mu.Unlock()

	for k, msg := range current {
		if previous[k] != msg {
			p.warn().Str("path", k).Str("reason", msg).Msg("configured path cannot be read")
		}
	}
	for k := range previous {
		if _, still := current[k]; !still {
			p.info().Str("path", k).Msg("configured path is readable again")
		}
	}
}

// The probe name rides every line: two probes on one file are otherwise
// indistinguishable in the log.
func (p *FileTailProbe) warn() *zerolog.Event {
	return p.moduleLogger.Warn().Str("probe", p.GetName())
}

func (p *FileTailProbe) info() *zerolog.Event {
	return p.moduleLogger.Info().Str("probe", p.GetName())
}

func (p *FileTailProbe) debug() *zerolog.Event {
	return p.moduleLogger.Debug().Str("probe", p.GetName())
}

// OnStart loads the bookmark store, performs the first glob expansion,
// and launches a rescan loop that picks up new files over time.
func (p *FileTailProbe) OnStart(quitChannel chan struct{}) error {
	bm, err := newBookmark(p.config.BookmarkPath)
	if err != nil {
		return fmt.Errorf("filetail: bookmark init: %w", err)
	}
	p.bookmarks = bm

	p.info().
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
	for _, ts := range p.tailing {
		tails = append(tails, ts.t)
	}
	p.tailing = map[string]*tailState{}
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
		p.warn().Msg("filetail shutdown deadline elapsed before all tails drained")
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
// any matched file not already being tailed. Paths that cannot be read are
// recorded and reported through Collect.
func (p *FileTailProbe) scanAndTail() {
	p.restartStalledTails()
	issues := map[string]string{}
	for _, pattern := range p.config.Paths {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			p.warn().Err(err).Str("pattern", pattern).Msg("invalid glob pattern; skipping")
			continue
		}
		if len(matches) == 0 {
			if msg := unmatchedIssue(pattern); msg != "" {
				issues[pattern] = msg
			}
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
			if err := p.startTail(abs); err != nil {
				issues[abs] = err.Error()
			}
		}
	}
	p.recordIssues(issues)
}

// unmatchedIssue explains why a pattern matched nothing when that is an
// error rather than "not there yet": a literal path is expected to exist
// for the service (a unit with ProtectHome=yes sees /home empty), and a
// glob whose literal directory cannot be listed is not merely empty.
func unmatchedIssue(pattern string) string {
	if !hasGlobMeta(pattern) {
		if _, err := os.Stat(pattern); err != nil {
			return describePathError(err)
		}
		return ""
	}
	dir := filepath.Dir(pattern)
	if hasGlobMeta(dir) {
		return ""
	}
	entries, err := os.Open(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return describePathError(err)
	}
	if err := entries.Close(); err != nil {
		return err.Error()
	}
	return ""
}

func describePathError(err error) string {
	switch {
	case os.IsNotExist(err):
		return "does not exist for the agent service"
	case os.IsPermission(err):
		return "permission denied"
	}
	return err.Error()
}

// startTail begins tailing file unless it is already tailed. It returns an
// error when the file exists but cannot be read; a file that vanished
// between the scan and here is left for the next rescan.
func (p *FileTailProbe) startTail(file string) error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	if _, exists := p.tailing[file]; exists {
		p.mu.Unlock()
		return nil
	}

	fi, err := os.Stat(file)
	if err != nil {
		p.mu.Unlock()
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: %w", describePathError(err), err)
	}
	size := fi.Size()
	opened := fi
	readable, err := os.Open(file)
	if err != nil {
		p.mu.Unlock()
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: %w", describePathError(err), err)
	}
	if err := readable.Close(); err != nil {
		p.debug().Err(err).Str("file", file).Msg("closing readability check failed")
	}
	fp := fingerprint(file, DefaultFingerprintLength)
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
		Poll: runtime.GOOS == "windows" || p.polling[file],
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
		p.warn().Err(err).Str("file", file).Msg("failed to start tail")
		return fmt.Errorf("failed to start tail: %w", err)
	}
	ts := &tailState{t: t, opened: opened}
	ts.offset.Store(offset)
	p.tailing[file] = ts
	p.wg.Add(1)
	p.mu.Unlock()

	// A file that produces no line during the run must still be
	// bookmarked: without an entry, or with the zero offset consume
	// would otherwise persist at stop, the next start reads it again.
	if err := p.bookmarks.Set(file, bookmarkEntry{Offset: offset, Fingerprint: fp}); err != nil {
		p.warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
	}

	go p.consume(file, ts, offset, fp, reopened)
	p.verifyStartedTail(file, ts)
	return nil
}

// startupVerifyDelay spaces the two looks verifyStartedTail takes at a new
// tail. It is short against the stall grace on purpose: the case it covers
// is a line written while the library has not yet registered its change
// watch, which the rescan-based check would only repair a minute later.
const startupVerifyDelay = time.Second

// verifyStartedTail covers the window in which nxadm/tail cannot see a
// write: it registers its change watch only after its first read reaches
// the end of the file, and a line appended in between raises no event, so
// on a quiet log it waits for the next write however far away. The tail is
// looked at twice, startupVerifyDelay apart; a file that grew while the
// tail read nothing over both looks is restarted at the offset already
// read, so nothing is replayed and nothing is lost. One timer per tail
// start, none while the tail runs.
func (p *FileTailProbe) verifyStartedTail(file string, ts *tailState) {
	time.AfterFunc(startupVerifyDelay, func() {
		if p.isStopped() {
			return
		}
		ts.divergence(file)
		time.AfterFunc(startupVerifyDelay, func() {
			if p.isStopped() {
				return
			}
			if reason, resume := ts.divergence(file); reason != "" && resume {
				p.restartStalledTail(file, ts, reason, resume)
			}
		})
	})
}

func (p *FileTailProbe) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// tailState is what the probe knows about one running tail beyond the
// tail itself: the file it opened and how far it has read, which is what
// lets the probe notice a tail that no longer follows the file at its path.
type tailState struct {
	t      *tail.Tail
	offset atomic.Int64

	mu            sync.Mutex
	opened        os.FileInfo
	mismatchSince time.Time
	checkedOffset int64

	superseded atomic.Bool
}

// markOpened records the file now at path as the one the tail reads.
func (ts *tailState) markOpened(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	ts.mu.Lock()
	ts.opened = fi
	ts.mu.Unlock()
}

// divergence says why the tail no longer follows the file at path, or "".
// resume is true when the tail is still on the right file, so a restart
// can continue at the offset already read instead of from the first byte.
// It never asks the tail: a tail stuck on a rotated file is exactly the
// one that would not answer.
func (ts *tailState) divergence(path string) (reason string, resume bool) {
	cur, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	offset := ts.offset.Load()
	ts.mu.Lock()
	opened := ts.opened
	unchanged := ts.checkedOffset == offset
	ts.checkedOffset = offset
	ts.mu.Unlock()
	if opened != nil && !os.SameFile(opened, cur) {
		return "the path names a different file than the one being read", false
	}
	if cur.Size() < offset {
		return "the file is shorter than the offset already read", false
	}
	if cur.Size() > offset && unchanged {
		return "the file grew and the tail read nothing of it", true
	}
	return "", false
}

// restartStalledTails restarts every tail that has not followed its file
// (rotated, truncated, or grown without a line read) for longer than the
// grace period: from the first byte when the file changed, at the offset
// already read when it did not. The library signals rotation through a shared
// per-path inotify channel that a second tail on the same path, or a lost
// event, leaves a tail waiting on for good, with no error and no line.
func (p *FileTailProbe) restartStalledTails() {
	type candidate struct {
		file   string
		ts     *tailState
		reason string
		resume bool
	}
	var due []candidate

	p.mu.Lock()
	for file, ts := range p.tailing {
		reason, resume := ts.divergence(file)
		ts.mu.Lock()
		switch {
		case reason == "":
			ts.mismatchSince = time.Time{}
		case ts.mismatchSince.IsZero():
			ts.mismatchSince = time.Now()
		case time.Since(ts.mismatchSince) >= p.stallGrace:
			due = append(due, candidate{file, ts, reason, resume})
		}
		ts.mu.Unlock()
	}
	p.mu.Unlock()

	for _, c := range due {
		p.restartStalledTail(c.file, c.ts, c.reason, c.resume)
	}
}

func (p *FileTailProbe) restartStalledTail(file string, ts *tailState, reason string, resume bool) {
	p.mu.Lock()
	if p.stopped || p.tailing[file] != ts {
		p.mu.Unlock()
		return
	}
	delete(p.tailing, file)
	ts.superseded.Store(true)
	p.polling[file] = true
	p.mu.Unlock()

	p.warn().Str("file", file).Str("reason", reason).
		Bool("resume_at_offset", resume).
		Msg("tail does not follow its file; restarting it")

	ts.t.Kill(nil)
	entry := bookmarkEntry{}
	if resume {
		entry = bookmarkEntry{Offset: ts.offset.Load(), Fingerprint: fingerprint(file, DefaultFingerprintLength)}
	}
	if err := p.bookmarks.Set(file, entry); err != nil {
		p.warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
	}
	if err := p.startTail(file); err != nil {
		p.warn().Err(err).Str("file", file).Msg("restarting stalled tail failed")
	}
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
func (p *FileTailProbe) consume(file string, ts *tailState, startOffset int64, fp string, reopened <-chan struct{}) {
	t := ts.t
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
			p.warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
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
			ts.offset.Store(0)
			ts.markOpened(file)
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
			p.debug().Err(line.Err).Str("file", file).Msg("tail line error")
			continue
		}
		lastOffset = line.SeekInfo.Offset
		ts.offset.Store(lastOffset)
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
	cause := t.Wait()
	if ts.superseded.Load() {
		return
	}
	persist()
	p.forgetDeadTail(file, ts, cause)
}

// forgetDeadTail drops a tail that ended on its own (the file vanished
// with its directory, a read error) so the next rescan starts a new one.
// Without it the dead tail stays registered and the file is never read
// again until the probe restarts.
func (p *FileTailProbe) forgetDeadTail(file string, ts *tailState, cause error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.tailing[file] != ts {
		return
	}
	delete(p.tailing, file)
	p.warn().Err(cause).Str("file", file).Msg("tail ended; retrying on next rescan")
}

func (p *FileTailProbe) publish(pc ParserConfig, line string, readTime time.Time, probeName, file string) {
	rec, ok := logparse.ParseLine(pc, line, readTime, probeName, ProbeType)
	if !ok {
		p.debug().Str("file", file).Str("line", logparse.Truncate(line, 200)).
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

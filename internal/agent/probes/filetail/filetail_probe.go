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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nxadm/tail"

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
	wg      sync.WaitGroup
	quit    chan struct{}
	stopped bool
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
		quit:         make(chan struct{}),
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

// GetTargetStrategies returns an empty list — like linux_logs, this
// probe publishes to the agentstate log channel, not the data_store
// router.
func (p *FileTailProbe) GetTargetStrategies() []string { return []string{} }

// ShouldStart always returns true; path resolution happens in OnStart.
func (p *FileTailProbe) ShouldStart() bool { return true }

// GetInterval is irrelevant for an event-driven probe but the poller
// requires a value.
func (p *FileTailProbe) GetInterval() time.Duration { return 5 * time.Minute }

// Collect is a no-op — tail goroutines publish records directly.
func (p *FileTailProbe) Collect() ([]data_store.DataPoint, error) { return nil, nil }

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
		// A literal (non-glob) path that does not yet exist still
		// deserves a tail with ReOpen so it is picked up when created.
		if len(matches) == 0 && !hasGlobMeta(pattern) {
			matches = []string{pattern}
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
	offset := resolveStartOffset(stored, hasStored, fp, size, p.config.FromBeginning)

	cfg := tail.Config{
		ReOpen:        true,
		Follow:        true,
		MustExist:     false,
		CompleteLines: true,
		Logger:        tail.DiscardingLogger,
	}
	if offset < 0 {
		cfg.Location = &tail.SeekInfo{Offset: 0, Whence: 2} // io.SeekEnd
	} else {
		cfg.Location = &tail.SeekInfo{Offset: offset, Whence: 0} // io.SeekStart
	}

	t, err := tail.TailFile(file, cfg)
	if err != nil {
		p.mu.Unlock()
		p.moduleLogger.Warn().Err(err).Str("file", file).Msg("failed to start tail")
		return
	}
	p.tailing[file] = t
	p.wg.Add(1)
	p.mu.Unlock()

	go p.consume(file, t)
}

// consume drains one file's tail channel, folds multiline records,
// parses each, publishes it, and periodically persists the offset.
func (p *FileTailProbe) consume(file string, t *tail.Tail) {
	defer p.wg.Done()

	asm := newMultilineAssembler(p.config.Multiline, p.config.MaxBytesPerLine)
	probeName := p.GetName()

	lastFlush := time.Now()
	var lastOffset int64

	persist := func() {
		fp := fingerprint(file, DefaultFingerprintLength)
		if err := p.bookmarks.Set(file, bookmarkEntry{Offset: lastOffset, Fingerprint: fp}); err != nil {
			p.moduleLogger.Warn().Err(err).Str("file", file).Msg("persisting bookmark failed")
		}
	}

	for line := range t.Lines {
		if line == nil {
			continue
		}
		if line.Err != nil {
			p.moduleLogger.Debug().Err(line.Err).Str("file", file).Msg("tail line error")
			continue
		}
		lastOffset = line.SeekInfo.Offset

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
		}
	}

	// Channel closed (tail stopped): flush any pending multiline record
	// and persist the final offset.
	for _, logical := range asm.Flush() {
		p.publish(p.config.Parser, logical, time.Now(), probeName, file)
	}
	persist()
}

func (p *FileTailProbe) publish(pc ParserConfig, line string, readTime time.Time, probeName, file string) {
	rec, ok := parseLine(pc, line, readTime, probeName, file)
	if !ok {
		p.moduleLogger.Debug().Str("file", file).Str("line", truncate(line, 200)).
			Msg("line did not parse as declared json; skipping")
		return
	}
	agentstate.PublishLog(rec)
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

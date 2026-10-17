// Package kuberneteslogs ships the stdout/stderr logs of every pod on the
// current Kubernetes node by following /var/log/pods. It auto-discovers pod
// log files, reassembles CRI/Docker partial chunks, applies per-file offset
// bookmarks, and publishes complete log records on the agent log rail.
package kuberneteslogs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/probes/logparse"
	"senhub-agent.go/internal/agent/probes/podlogs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/probesdk/logs"
	"senhub-agent.go/probesdk/state"
)

const ProbeType = "kubernetes_logs"

type publishFunc func(logs.Record)

type liveFile struct {
	path      string
	ref       podlogs.PodRef
	inode     uint64
	offset    int64
	assembler *podlogs.Assembler
	// folds holds one multiline fold per stream: stdout and stderr
	// interleave in the same file and must not be folded together.
	folds map[string]*fold
	// idle is set when a cycle read no new byte, so a record left
	// pending by the last line of a burst is not held forever.
	idle bool
}

// fold is a multiline assembler plus the entry that opened the record it
// is building: a folded record takes that entry's time and stream.
type fold struct {
	asm   *logparse.Assembler
	first podlogs.Entry
}

type containerKey struct {
	namespace string
	pod       string
	uid       string
	container string
	restart   int
}

func keyForRef(r podlogs.PodRef) containerKey {
	return containerKey{
		namespace: r.Namespace,
		pod:       r.Pod,
		uid:       r.UID,
		container: r.Container,
		restart:   r.RestartCount,
	}
}

type Probe struct {
	*types.BaseProbe

	config       Config
	moduleLogger *logger.ModuleLogger
	publish      publishFunc

	nodeName string
	self     podlogs.PodRef
	filter   podlogs.Filter

	prevScan  *podlogs.Scan
	files     map[string]*liveFile
	bookmarks bookmarkStore

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	shutdownOnce sync.Once

	recordsEmitted   atomic.Uint64
	recordsTruncated atomic.Uint64
	readErrors       atomic.Uint64
	filesFollowed    atomic.Uint64
	podsFollowed     atomic.Uint64
}

func NewKubernetesLogsProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.kubernetes_logs")

	parsed, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	nodeName := os.Getenv("SENHUB_K8S_NODE_NAME")
	self := podlogs.PodRef{
		Namespace: os.Getenv("SENHUB_K8S_POD_NAMESPACE"),
		Pod:       os.Getenv("SENHUB_K8S_POD_NAME"),
		UID:       os.Getenv("SENHUB_K8S_POD_UID"),
	}

	filter := podlogs.Filter{
		IncludeNamespaces: parsed.IncludeNamespaces,
		ExcludeNamespaces: parsed.ExcludeNamespaces,
		IncludePods:       parsed.IncludePods,
		ExcludePods:       parsed.ExcludePods,
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Probe{
		BaseProbe:    &types.BaseProbe{},
		config:       parsed,
		moduleLogger: moduleLogger,
		publish:      logs.Publish,
		nodeName:     nodeName,
		self:         self,
		filter:       filter,
		files:        map[string]*liveFile{},
		bookmarks:    noopBookmark{},
		ctx:          ctx,
		cancel:       cancel,
	}

	if nodeName == "" {
		p.warn().Msg("SENHUB_K8S_NODE_NAME is not set; every record will have an empty k8s.node.name attribute")
	}

	p.SetProbeType(ProbeType)
	return p, nil
}

func (p *Probe) ShouldStart() bool { return true }

func (p *Probe) GetInterval() time.Duration { return 1 * time.Minute }

func (p *Probe) OnStart(quitChannel chan struct{}) error {
	if p.config.BookmarkPath == "" {
		p.config.BookmarkPath = state.Path(p.GetName() + ".bookmarks.json")
	}
	bookmarks, err := newBookmark(p.config.BookmarkPath)
	if err != nil {
		return fmt.Errorf("kubernetes_logs: bookmark init: %w", err)
	}
	p.bookmarks = bookmarks

	p.info().
		Str("root", p.config.Root).
		Dur("scan_interval", p.config.ScanInterval).
		Str("bookmark_path", p.config.BookmarkPath).
		Bool("from_beginning", p.config.FromBeginning).
		Msg("Starting kubernetes_logs probe")

	p.scanOnce()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.run(quitChannel)
	}()

	return nil
}

func (p *Probe) OnShutdown(_ context.Context) error {
	p.shutdownOnce.Do(func() {
		p.cancel()
		p.wg.Wait()

		if err := p.bookmarks.Flush(); err != nil {
			p.warn().Err(err).Msg("cannot flush kubernetes_logs bookmarks on shutdown")
		}
	})
	return nil
}

func (p *Probe) run(quit <-chan struct{}) {
	ticker := time.NewTicker(p.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-quit:
			return
		case <-ticker.C:
			p.scanOnce()
		}
	}
}

func (p *Probe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points := []data_store.DataPoint{
		{Name: "senhub.kubernetes_logs.files_followed", Value: float64(p.filesFollowed.Load()), Timestamp: now},
		{Name: "senhub.kubernetes_logs.pods_followed", Value: float64(p.podsFollowed.Load()), Timestamp: now},
		{Name: "senhub.kubernetes_logs.records_emitted", Value: float64(p.recordsEmitted.Load()), Timestamp: now},
		{Name: "senhub.kubernetes_logs.records_truncated", Value: float64(p.recordsTruncated.Load()), Timestamp: now},
		{Name: "senhub.kubernetes_logs.read_errors", Value: float64(p.readErrors.Load()), Timestamp: now},
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *Probe) scanOnce() {
	scan, err := podlogs.ScanPods(os.DirFS(p.config.Root), ".", p.filter, p.self)
	if err != nil {
		p.readErrors.Add(1)
		p.warn().Err(err).Str("root", p.config.Root).Msg("cannot scan pod log root")
		return
	}
	p.normalizeScan(&scan)

	if p.prevScan == nil {
		p.prevScan = &scan
		for _, f := range scan.Files {
			p.files[f.Path] = p.newLiveFile(f, p.initialOffset(f, true))
		}

		p.readCurrentFiles(scan.Files)
		p.updateMetrics(scan)
		p.persistBookmarks(scan.Files)
		p.flushBookmarksIfDue()
		return
	}

	prevScan := *p.prevScan
	changes := podlogs.Diff(prevScan, scan)
	handled := map[string]bool{}

	for _, ch := range changes {
		switch ch.Kind {
		case podlogs.Added:
			if _, exists := p.files[ch.After.Path]; !exists {
				p.files[ch.After.Path] = p.newLiveFile(ch.After, 0)
			}
		case podlogs.Truncated:
			st := p.files[ch.After.Path]
			if st == nil {
				p.files[ch.After.Path] = p.newLiveFile(ch.After, 0)
				continue
			}
			st.offset = 0
			st.assembler = p.newAssembler()
			st.ref = ch.After.Ref
			st.inode = ch.After.Inode
		case podlogs.Rotated:
			p.handleRotation(ch, scan, prevScan)
			handled[ch.Before.Path] = true
		case podlogs.Removed:
			if st := p.files[ch.Before.Path]; st != nil {
				p.flushAssembler(st)
				delete(p.files, ch.Before.Path)
				p.bookmarks.Delete(ch.Before.Path)
			}
			handled[ch.Before.Path] = true
		}
	}

	for _, prevFile := range prevScan.Files {
		if handled[prevFile.Path] {
			continue
		}
		if hasCurrentWithKey(scan.Files, keyForRef(prevFile.Ref)) {
			continue
		}
		if _, ok := rotatedByPath(scan.Rotated, prevFile.Path); !ok {
			continue
		}

		st := p.files[prevFile.Path]
		if st == nil {
			continue
		}

		_ = p.readFile(st)
		p.flushAssembler(st)
		delete(p.files, prevFile.Path)
		p.bookmarks.Delete(prevFile.Path)
	}

	for _, f := range scan.Files {
		st := p.files[f.Path]
		if st == nil {
			p.files[f.Path] = p.newLiveFile(f, 0)
			continue
		}
		st.ref = f.Ref
		st.inode = f.Inode
	}

	p.readCurrentFiles(scan.Files)
	p.cleanupStaleFiles(scan.Files)

	p.prevScan = &scan
	p.updateMetrics(scan)
	p.persistBookmarks(scan.Files)
	p.flushBookmarksIfDue()
}

func (p *Probe) handleRotation(ch podlogs.Change, cur podlogs.Scan, prev podlogs.Scan) {
	st := p.files[ch.Before.Path]
	offset := int64(0)
	if st != nil {
		offset = st.offset
	}

	if rot, ok := findRotatedSuccessor(ch.Before, cur, prev); ok {
		rotState := p.newLiveFile(rot, offset)
		if st != nil {
			rotState.assembler = st.assembler
			rotState.folds = st.folds
		}
		_ = p.readFile(rotState)
		p.flushAssembler(rotState)
	} else if st != nil {
		p.flushAssembler(st)
	}

	delete(p.files, ch.Before.Path)
	p.bookmarks.Delete(ch.Before.Path)

	p.files[ch.After.Path] = p.newLiveFile(ch.After, 0)
	p.bookmarks.Delete(ch.After.Path)
}

func (p *Probe) readCurrentFiles(files []podlogs.FileState) {
	for _, f := range files {
		if st := p.files[f.Path]; st != nil {
			if err := p.readFile(st); err != nil {
				p.debug().Str("path", f.Path).Err(err).Msg("cannot read pod log file completely")
			}
		}
	}
}

func (p *Probe) readFile(st *liveFile) error {
	f, err := os.Open(st.path)
	if err != nil {
		p.recordReadError(st.path, err)
		return err
	}
	defer f.Close()

	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		p.recordReadError(st.path, err)
		return err
	}

	reader := bufio.NewReader(f)
	start := st.offset
	defer func() {
		st.idle = st.offset == start
		if st.idle {
			p.flushFolds(st)
		}
	}()

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			if strings.HasSuffix(line, "\n") {
				raw := line[:len(line)-1]
				if strings.HasSuffix(raw, "\r") {
					raw = raw[:len(raw)-1]
				}
				st.offset += int64(len(line))
				p.processLine(st, raw)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			p.recordReadError(st.path, readErr)
			return readErr
		}
	}
}

func (p *Probe) processLine(st *liveFile, raw string) {
	entry, ok := podlogs.ParseLine(raw)
	if !ok {
		return
	}

	complete, ok := st.assembler.Add(entry)
	if ok {
		p.foldEntry(st, complete)
	}
}

func (p *Probe) foldEntry(st *liveFile, entry podlogs.Entry) {
	f := st.folds[entry.Stream]
	if f == nil {
		f = &fold{asm: logparse.NewAssembler(p.config.Multiline, p.config.MaxLineBytes)}
		if st.folds == nil {
			st.folds = map[string]*fold{}
		}
		st.folds[entry.Stream] = f
	}
	before := f.asm.Pending()
	out := f.asm.Append(entry.Message)
	after := f.asm.Pending()

	meta := entry
	if before {
		meta = f.first
	}
	for _, msg := range out {
		rec := meta
		rec.Message = msg
		p.publishEntry(st, rec)
	}
	if after && (!before || len(out) > 0) {
		f.first = entry
	}
}

func (p *Probe) flushFolds(st *liveFile) {
	for _, f := range st.folds {
		if !f.asm.Pending() {
			continue
		}
		for _, msg := range f.asm.Flush() {
			rec := f.first
			rec.Message = msg
			p.publishEntry(st, rec)
		}
	}
}

func (p *Probe) publishEntry(st *liveFile, entry podlogs.Entry) {
	if entry.Truncated {
		p.recordsTruncated.Add(1)
	}

	rec, ok := logparse.ParseLine(p.config.Parser, entry.Message, entry.Time, p.GetName(), ProbeType)
	if !ok {
		return
	}

	rec.Timestamp = entry.Time
	rec.Body = entry.Message
	if rec.Attributes == nil {
		rec.Attributes = map[string]string{}
	}
	for k, v := range st.ref.Attributes() {
		rec.Attributes[k] = v
	}
	rec.Attributes["k8s.node.name"] = p.nodeName
	rec.Attributes["log.iostream"] = entry.Stream
	rec.Attributes["log.file.path"] = st.path
	rec.ProducerProbeName = p.GetName()
	rec.ProducerProbeType = ProbeType

	p.recordsEmitted.Add(1)
	p.publish(rec)
}

func (p *Probe) flushAssembler(st *liveFile) {
	for _, entry := range st.assembler.Flush() {
		p.foldEntry(st, entry)
	}
	p.flushFolds(st)
}

func (p *Probe) newAssembler() *podlogs.Assembler {
	return &podlogs.Assembler{MaxBytes: p.config.MaxLineBytes}
}

func (p *Probe) newLiveFile(f podlogs.FileState, offset int64) *liveFile {
	return &liveFile{
		path:      f.Path,
		ref:       f.Ref,
		inode:     f.Inode,
		offset:    offset,
		assembler: p.newAssembler(),
	}
}

func (p *Probe) initialOffset(f podlogs.FileState, first bool) int64 {
	if first {
		if e, ok := p.bookmarks.Get(f.Path); ok && e.Inode == f.Inode {
			if e.Offset <= f.Size {
				return e.Offset
			}
			return 0
		}

		if p.config.FromBeginning {
			return 0
		}
		return f.Size
	}

	return 0
}

func (p *Probe) normalizeScan(scan *podlogs.Scan) {
	for i := range scan.Files {
		scan.Files[i].Path = p.fullPath(scan.Files[i].Path)
	}
	for i := range scan.Rotated {
		scan.Rotated[i].Path = p.fullPath(scan.Rotated[i].Path)
	}
}

func (p *Probe) fullPath(rel string) string {
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	return filepath.Join(p.config.Root, rel)
}

func (p *Probe) updateMetrics(scan podlogs.Scan) {
	p.filesFollowed.Store(uint64(len(scan.Files)))

	podSet := map[string]struct{}{}
	for _, f := range scan.Files {
		podSet[f.Ref.Namespace+"\x00"+f.Ref.Pod+"\x00"+f.Ref.UID] = struct{}{}
	}
	p.podsFollowed.Store(uint64(len(podSet)))
}

func (p *Probe) persistBookmarks(files []podlogs.FileState) {
	for _, f := range files {
		if st := p.files[f.Path]; st != nil {
			p.bookmarks.Set(f.Path, bookmarkEntry{Inode: st.inode, Offset: st.offset})
		}
	}
}

func (p *Probe) cleanupStaleFiles(current []podlogs.FileState) {
	currentPaths := make(map[string]bool, len(current))
	for _, f := range current {
		currentPaths[f.Path] = true
	}

	for path, st := range p.files {
		if currentPaths[path] {
			continue
		}
		p.flushAssembler(st)
		delete(p.files, path)
		p.bookmarks.Delete(path)
	}
}

func (p *Probe) flushBookmarksIfDue() {
	if err := p.bookmarks.FlushIfDue(time.Now()); err != nil {
		p.warn().Err(err).Msg("cannot flush kubernetes_logs bookmarks")
	}
}

func (p *Probe) recordReadError(path string, err error) {
	p.readErrors.Add(1)
	p.warn().Str("path", path).Err(err).Msg("cannot read pod log file")
}

func findRotatedSuccessor(before podlogs.FileState, cur podlogs.Scan, prev podlogs.Scan) (podlogs.FileState, bool) {
	prevRotated := make(map[string]bool, len(prev.Rotated))
	for _, f := range prev.Rotated {
		prevRotated[f.Path] = true
	}

	key := keyForRef(before.Ref)
	var best podlogs.FileState
	found := false

	for _, f := range cur.Rotated {
		if keyForRef(f.Ref) != key {
			continue
		}
		if prevRotated[f.Path] {
			continue
		}
		if !found || f.ModTime.After(best.ModTime) || (f.ModTime.Equal(best.ModTime) && f.Path > best.Path) {
			best = f
			found = true
		}
	}

	return best, found
}

func hasCurrentWithKey(files []podlogs.FileState, key containerKey) bool {
	for _, f := range files {
		if keyForRef(f.Ref) == key {
			return true
		}
	}
	return false
}

func rotatedByPath(rotated []podlogs.FileState, path string) (podlogs.FileState, bool) {
	for _, f := range rotated {
		if f.Path == path {
			return f, true
		}
	}
	return podlogs.FileState{}, false
}

func (p *Probe) info() *zerolog.Event {
	return p.moduleLogger.Info().Str("probe", p.GetName())
}

func (p *Probe) warn() *zerolog.Event {
	return p.moduleLogger.Warn().Str("probe", p.GetName())
}

func (p *Probe) debug() *zerolog.Event {
	return p.moduleLogger.Debug().Str("probe", p.GetName())
}

//go:build windows

package windowseventlog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// This file is the Windows-only wevtapi binding behind the eventReader
// type the OS-agnostic probe code drives. It uses the pull model of
// EvtSubscribe: each channel gets its own subscription with a signalled
// kernel event; a goroutine waits on that event (or the shared stop
// event) and drains ready events with EvtNext, renders each to XML with
// EvtRender, then hands the XML to the OS-agnostic parse/filter/publish
// path. A per-channel bookmark is advanced after every drained event and
// the bookmark set is flushed to disk so restarts resume without
// duplication or loss.
//
// Runtime-validated on Windows Server 2022 (build 20348): real System +
// Application events flow end-to-end to OTLP logs with all attributes;
// per-channel bookmarks persist and resume after restart with no
// duplication and no loss; steady-state tail footprint ~22 MB / ~0% CPU
// (a 53k-event backlog flood drains at ~4400/s, ~1 core, 83 MB peak).
//
// Note the signal model: wevtapi's pull subscription needs the signal
// event INITIALLY SIGNALLED to drive the first drain, and the wait below
// uses a poll_interval timeout so draining never depends solely on
// wevtapi re-signalling. A non-signalled event + INFINITE wait drains
// nothing (the bug the live validation caught).

// wevtapi entry points, resolved lazily on first use so the package
// links on any Windows SKU (the DLL ships with every supported Windows).
var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")

	procEvtSubscribe             = modwevtapi.NewProc("EvtSubscribe")
	procEvtNext                  = modwevtapi.NewProc("EvtNext")
	procEvtRender                = modwevtapi.NewProc("EvtRender")
	procEvtClose                 = modwevtapi.NewProc("EvtClose")
	procEvtCreateBookmark        = modwevtapi.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark        = modwevtapi.NewProc("EvtUpdateBookmark")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtFormatMessage         = modwevtapi.NewProc("EvtFormatMessage")
)

// EvtSubscribe flags (winevt.h EVT_SUBSCRIBE_FLAGS).
const (
	evtSubscribeToFutureEvents      = 1
	evtSubscribeStartAtOldestRecord = 2
	evtSubscribeStartAfterBookmark  = 3
)

// EvtRender flags (winevt.h EVT_RENDER_FLAGS).
const (
	evtRenderEventXml     = 1
	evtRenderBookmark     = 2
	evtFormatMessageEvent = 1
)

// drainBatchSize bounds one EvtNext call. 64 keeps the per-call render
// loop short so the stop event is observed promptly even on a busy
// channel.
const drainBatchSize = 64

// publisherMetadataCacheMax bounds the number of cached publisher
// metadata handles. Publisher metadata handles are light but not free,
// and the set of providers on a host is usually small.
const publisherMetadataCacheMax = 64

// publisherMetadataEntry is a cached EvtOpenPublisherMetadata handle
// with a small reference count. A handle may be evicted only when no
// goroutine is holding a reference to it.
type publisherMetadataEntry struct {
	handle windows.Handle
	refs   int
}

// publisherMetadataCache gives the Windows subscription a bounded,
// provider-keyed metadata handle cache. Failed opens are remembered so
// they are not retried on every event from the same provider.
type publisherMetadataCache struct {
	mu           sync.Mutex
	handles      map[string]*publisherMetadataEntry
	openFailed   map[string]bool
	formatFailed map[string]bool
}

func newPublisherMetadataCache() *publisherMetadataCache {
	return &publisherMetadataCache{
		handles:      make(map[string]*publisherMetadataEntry),
		openFailed:   make(map[string]bool),
		formatFailed: make(map[string]bool),
	}
}

// acquire returns a publisher metadata handle and a release function.
// The release function must be called exactly once when formatting is
// done. alreadyFailed is true when this provider had previously failed
// to open; the caller uses that to log only once per provider.
func (c *publisherMetadataCache) acquire(provider string) (windows.Handle, func(), error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.openFailed[provider] {
		return 0, nil, fmt.Errorf("publisher metadata previously failed to open for %q", provider), true
	}

	if entry, ok := c.handles[provider]; ok {
		entry.refs++
		return entry.handle, func() { c.release(provider) }, nil, false
	}

	h, err := evtOpenPublisherMetadata(provider)
	if err != nil {
		c.openFailed[provider] = true
		return 0, nil, fmt.Errorf("open publisher metadata %q: %w", provider, err), false
	}

	if len(c.handles) >= publisherMetadataCacheMax {
		for oldProvider, oldEntry := range c.handles {
			if oldEntry.refs == 0 {
				evtClose(oldEntry.handle)
				delete(c.handles, oldProvider)
				break
			}
		}
	}

	// If all cached handles are in use, do not cache this one; keeping
	// the map bounded matters more than caching a bursty extra provider.
	// The caller closes the temporary handle through its release func.
	if len(c.handles) >= publisherMetadataCacheMax {
		return h, func() { evtClose(h) }, nil, false
	}

	c.handles[provider] = &publisherMetadataEntry{handle: h, refs: 1}
	return h, func() { c.release(provider) }, nil, false
}

func (c *publisherMetadataCache) release(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.handles[provider]; ok {
		entry.refs--
		if entry.refs < 0 {
			entry.refs = 0
		}
	}
}

func (c *publisherMetadataCache) markFormatFailed(provider string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.formatFailed[provider] {
		return true
	}
	c.formatFailed[provider] = true
	return false
}

func (c *publisherMetadataCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for provider, entry := range c.handles {
		evtClose(entry.handle)
		delete(c.handles, provider)
	}
}

// eventReader holds one subscription goroutine per configured channel,
// a shared stop event to unblock them, and the bookmark store they flush
// progress into.
type eventReader struct {
	cfg        WindowsEventLogProbeConfig
	log        *logger.ModuleLogger
	probeName  string
	logTargets []string
	emitted    *atomic.Uint64

	stopEvent windows.Handle
	bookmarks *bookmarkStore
	metadata  *publisherMetadataCache

	wg       sync.WaitGroup
	stopOnce sync.Once
}

// newEventReader opens a subscription per channel and starts draining.
// Returns an error only for unrecoverable setup failures (event creation,
// bookmark load); a single channel that fails to subscribe is logged and
// skipped so one bad channel name does not sink the whole probe.
func newEventReader(cfg WindowsEventLogProbeConfig, log *logger.ModuleLogger, probeName string, logTargets []string, emitted *atomic.Uint64) (*eventReader, error) {
	stop, err := windows.CreateEvent(nil, 1 /*manual reset*/, 0 /*nonsignalled*/, nil)
	if err != nil {
		return nil, fmt.Errorf("create stop event: %w", err)
	}

	store, err := newBookmarkStore(cfg.BookmarkPath)
	if err != nil {
		// Non-fatal: a corrupt/unreadable bookmark file means we resume
		// from now (or backlog) instead of after the last record. Log it
		// loudly — silent loss of bookmark state is exactly what the
		// persistence is meant to prevent.
		log.Warn().Err(err).Str("bookmark_path", cfg.BookmarkPath).
			Msg("Could not load bookmark state; starting without resume point")
	}

	r := &eventReader{
		cfg:        cfg,
		log:        log,
		probeName:  probeName,
		logTargets: logTargets,
		emitted:    emitted,
		stopEvent:  stop,
		bookmarks:  store,
		metadata:   newPublisherMetadataCache(),
	}

	for _, channel := range cfg.Channels {
		r.wg.Add(1)
		go r.runChannel(channel)
	}
	return r, nil
}

// runChannel owns one channel subscription for the probe's lifetime. It
// blocks on the channel's signal event (set by wevtapi when events are
// ready) or the shared stop event, draining ready events on each signal.
func (r *eventReader) runChannel(channel string) {
	defer r.wg.Done()

	// Manual-reset, INITIALLY SIGNALED. wevtapi's pull model relies on
	// the initial signaled state to drive the first drain (it does not
	// reliably re-signal a manual-reset event created non-signalled for
	// the pre-existing backlog). We also wait with a poll_interval
	// timeout below, so draining never depends solely on wevtapi setting
	// the event — the timeout guarantees liveness, the signal just makes
	// new events prompt.
	signal, err := windows.CreateEvent(nil, 1 /*manual reset*/, 1 /*initially signalled*/, nil)
	if err != nil {
		r.log.Error().Err(err).Str("channel", channel).Msg("create signal event")
		return
	}
	defer windows.CloseHandle(signal)

	// A bookmark handle is always created so EvtUpdateBookmark has a
	// target even when we subscribe to future events. If a persisted
	// bookmark exists we resume after it; otherwise honour backlog vs
	// tail-from-now.
	flags := uint32(evtSubscribeToFutureEvents)
	if r.cfg.Backlog {
		flags = evtSubscribeStartAtOldestRecord
	}

	bmXML := r.bookmarks.get(channel)
	bookmark, err := evtCreateBookmark(bmXML)
	if err != nil {
		r.log.Error().Err(err).Str("channel", channel).Msg("create bookmark")
		return
	}
	defer evtClose(bookmark)

	// A non-empty persisted bookmark wins over backlog/tail: resume
	// exactly where we left off.
	subBookmark := windows.Handle(0)
	if bmXML != "" {
		flags = evtSubscribeStartAfterBookmark
		subBookmark = bookmark
	}

	query := buildXPathQuery(r.cfg.levelInts)
	sub, err := evtSubscribe(signal, channel, query, subBookmark, flags)
	if err != nil {
		r.log.Error().Err(err).Str("channel", channel).Str("query", query).
			Msg("EvtSubscribe failed; channel skipped (check name and privileges — Security needs Event Log Readers)")
		return
	}
	defer evtClose(sub)

	r.log.Info().Str("channel", channel).Uint32("flags", flags).
		Bool("resumed", bmXML != "").Msg("Subscribed to Event Log channel")

	// Wait timeout = poll_interval: we drain on every signal AND on every
	// timeout tick, so events flow even if wevtapi never re-signals.
	pollMs := uint32(r.cfg.PollInterval / time.Millisecond)
	if pollMs < 1000 {
		pollMs = 1000
	}

	waitHandles := []windows.Handle{signal, r.stopEvent}
	for {
		w, err := windows.WaitForMultipleObjects(waitHandles, false, pollMs)
		switch {
		case err != nil && w != uint32(windows.WAIT_TIMEOUT):
			r.log.Error().Err(err).Str("channel", channel).Msg("wait failed; stopping channel")
			return
		case w == windows.WAIT_OBJECT_0+1: // stop event
			return
		case w == windows.WAIT_OBJECT_0: // signal: events ready
			r.drain(channel, sub, bookmark)
			windows.ResetEvent(signal)
		default: // WAIT_TIMEOUT (or benign): poll for events anyway
			r.drain(channel, sub, bookmark)
		}
	}
}

// drain reads every ready event from the subscription, rendering,
// filtering and publishing each, then flushes the advanced bookmark. It
// returns when EvtNext reports no more items.
func (r *eventReader) drain(channel string, sub, bookmark windows.Handle) {
	advanced := false
	emitted := 0
	for {
		events, err := evtNext(sub, drainBatchSize)
		if err != nil {
			// ERROR_NO_MORE_ITEMS is the normal end of a drain.
			if !errors.Is(err, windows.ERROR_NO_MORE_ITEMS) && !errors.Is(err, windows.ERROR_TIMEOUT) {
				r.log.Debug().Err(err).Str("channel", channel).Msg("EvtNext ended")
			}
			break
		}
		for _, ev := range events {
			emitted++
			if xmlStr, rerr := evtRenderXML(ev); rerr != nil {
				r.log.Debug().Err(rerr).Str("channel", channel).Msg("EvtRender failed; event skipped")
			} else {
				parsed, ok := parseEventXML(xmlStr)
				if !ok {
					r.log.Debug().Str("channel", channel).Str("xml", truncate(xmlStr, 200)).
						Msg("Unparseable event XML; skipped")
				} else if r.cfg.shouldEmit(parsed) {
					if parsed.MessageFromFallback {
						if msg, ok := r.renderEventMessage(ev, parsed.Provider); ok {
							parsed.Message = msg
						}
					}
					rec := parsed.toLogRecord(r.probeName, r.cfg.RedactPII)
					rec.TargetStrategies = r.logTargets
					agentstate.PublishLog(rec)
					if r.emitted != nil {
						r.emitted.Add(1)
					}
				}
			}

			if uerr := evtUpdateBookmark(bookmark, ev); uerr != nil {
				r.log.Debug().Err(uerr).Str("channel", channel).Msg("EvtUpdateBookmark failed")
			} else {
				advanced = true
			}
			evtClose(ev)
		}
	}

	if !advanced {
		return
	}
	r.log.Debug().Str("channel", channel).Int("events", emitted).Msg("drained event batch")
	if bmXML, perr := evtRenderBookmarkXML(bookmark); perr != nil {
		r.log.Debug().Err(perr).Str("channel", channel).Msg("render bookmark failed")
	} else {
		r.bookmarks.set(channel, bmXML)
		if werr := r.bookmarks.persist(); werr != nil {
			r.log.Warn().Err(werr).Str("channel", channel).Msg("persist bookmark failed")
		}
	}
}

// renderEventMessage asks wevtapi for the publisher's formatted message
// for the current event. It is called only when the XML rendering had no
// RenderingInfo.Message. Failures fall back to the parser's compact
// provider-and-data message; each distinct provider is logged once.
func (r *eventReader) renderEventMessage(event windows.Handle, provider string) (string, bool) {
	if provider == "" {
		return "", false
	}

	publisher, release, err, alreadyFailed := r.metadata.acquire(provider)
	if err != nil {
		if !alreadyFailed {
			r.log.Debug().Err(err).Str("provider", provider).
				Msg("EvtOpenPublisherMetadata failed; using fallback message")
		}
		return "", false
	}
	defer release()

	message, err := evtFormatMessage(publisher, event)
	if err != nil {
		if !r.metadata.markFormatFailed(provider) {
			r.log.Debug().Err(err).Str("provider", provider).
				Msg("EvtFormatMessage failed; using fallback message")
		}
		return "", false
	}
	return message, true
}

// stop signals every channel goroutine to exit and waits for them within
// the caller's deadline (5s default), then closes the stop event and
// flushes a final bookmark snapshot.
func (r *eventReader) stop(ctx context.Context) error {
	r.stopOnce.Do(func() {
		_ = windows.SetEvent(r.stopEvent)
	})

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	var stopErr error
	select {
	case <-done:
	case <-time.After(time.Until(deadline)):
		stopErr = fmt.Errorf("windows_eventlog: channel goroutines did not stop before deadline")
	}

	if stopErr == nil {
		r.metadata.close()
	}
	_ = windows.CloseHandle(r.stopEvent)
	if perr := r.bookmarks.persist(); perr != nil && stopErr == nil {
		stopErr = fmt.Errorf("final bookmark persist: %w", perr)
	}
	return stopErr
}

// --- thin wevtapi wrappers -------------------------------------------------

func evtSubscribe(signal windows.Handle, channel, query string, bookmark windows.Handle, flags uint32) (windows.Handle, error) {
	chPtr, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return 0, fmt.Errorf("encode channel: %w", err)
	}
	var qPtr *uint16
	if query != "" {
		if qPtr, err = windows.UTF16PtrFromString(query); err != nil {
			return 0, fmt.Errorf("encode query: %w", err)
		}
	}
	r, _, e := procEvtSubscribe.Call(
		0,                              // Session = NULL (local)
		uintptr(signal),                // SignalEvent
		uintptr(unsafe.Pointer(chPtr)), // ChannelPath
		uintptr(unsafe.Pointer(qPtr)),  // Query (NULL = all)
		uintptr(bookmark),              // Bookmark (0 unless StartAfterBookmark)
		0,                              // Context
		0,                              // Callback = NULL (pull model)
		uintptr(flags),                 // Flags
	)
	if r == 0 {
		return 0, e
	}
	return windows.Handle(r), nil
}

// evtNext pulls up to max ready events. Returns ERROR_NO_MORE_ITEMS when
// the subscription is drained — the caller treats that as the normal
// loop exit, not a failure.
func evtNext(sub windows.Handle, max int) ([]windows.Handle, error) {
	events := make([]windows.Handle, max)
	var returned uint32
	r, _, e := procEvtNext.Call(
		uintptr(sub),
		uintptr(max),
		uintptr(unsafe.Pointer(&events[0])),
		0, // Timeout = 0: do not block, we already waited on the signal
		0, // Flags reserved
		uintptr(unsafe.Pointer(&returned)),
	)
	if r == 0 {
		return nil, e
	}
	return events[:returned], nil
}

// evtRenderXML renders an event handle to its Event-schema XML document.
// Two-call pattern: the first sizes the buffer (returns
// ERROR_INSUFFICIENT_BUFFER), the second fills it.
func evtRenderXML(fragment windows.Handle) (string, error) {
	var bufferUsed, propertyCount uint32
	procEvtRender.Call(
		0,
		uintptr(fragment),
		evtRenderEventXml,
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	if bufferUsed == 0 {
		return "", fmt.Errorf("EvtRender returned zero buffer size")
	}
	buf := make([]uint16, (bufferUsed/2)+1)
	r, _, e := procEvtRender.Call(
		0,
		uintptr(fragment),
		evtRenderEventXml,
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	if r == 0 {
		return "", e
	}
	return windows.UTF16ToString(buf), nil
}

// evtRenderBookmarkXML serialises a bookmark handle to the opaque XML we
// persist and feed back to EvtCreateBookmark on the next start.
func evtRenderBookmarkXML(bookmark windows.Handle) (string, error) {
	var bufferUsed, propertyCount uint32
	procEvtRender.Call(
		0,
		uintptr(bookmark),
		evtRenderBookmark,
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	if bufferUsed == 0 {
		return "", fmt.Errorf("EvtRender(bookmark) returned zero buffer size")
	}
	buf := make([]uint16, (bufferUsed/2)+1)
	r, _, e := procEvtRender.Call(
		0,
		uintptr(bookmark),
		evtRenderBookmark,
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	if r == 0 {
		return "", e
	}
	return windows.UTF16ToString(buf), nil
}

// evtCreateBookmark creates a bookmark handle, optionally seeded from
// persisted XML (empty string = fresh bookmark).
func evtCreateBookmark(xml string) (windows.Handle, error) {
	var p *uint16
	if xml != "" {
		var err error
		if p, err = windows.UTF16PtrFromString(xml); err != nil {
			return 0, fmt.Errorf("encode bookmark xml: %w", err)
		}
	}
	r, _, e := procEvtCreateBookmark.Call(uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return 0, e
	}
	return windows.Handle(r), nil
}

func evtUpdateBookmark(bookmark, event windows.Handle) error {
	r, _, e := procEvtUpdateBookmark.Call(uintptr(bookmark), uintptr(event))
	if r == 0 {
		return e
	}
	return nil
}

// evtOpenPublisherMetadata opens provider metadata for a provider name.
// The provider name is the PublisherId used by EvtFormatMessage.
func evtOpenPublisherMetadata(provider string) (windows.Handle, error) {
	providerPtr, err := windows.UTF16PtrFromString(provider)
	if err != nil {
		return 0, fmt.Errorf("encode provider: %w", err)
	}
	r, _, e := procEvtOpenPublisherMetadata.Call(
		0,                                    // Session = NULL (local)
		uintptr(unsafe.Pointer(providerPtr)), // PublisherId
		0,                                    // LogFilePath = NULL
		0,                                    // Locale = default
		0,                                    // Flags
	)
	if r == 0 {
		return 0, fmt.Errorf("EvtOpenPublisherMetadata: %w", e)
	}
	return windows.Handle(r), nil
}

// evtFormatMessage renders a publisher's formatted event message.
// Two-call pattern: the first call with a zero-size buffer returns
// ERROR_INSUFFICIENT_BUFFER and the required byte count.
func evtFormatMessage(publisherMetadata, event windows.Handle) (string, error) {
	var bufferUsed uint32
	r, _, e := procEvtFormatMessage.Call(
		uintptr(publisherMetadata),
		uintptr(event),
		0, // MessageId
		0, // ValueCount
		0, // Values
		evtFormatMessageEvent,
		0, // BufferSize
		0, // Buffer
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	if r == 0 && !errors.Is(e, windows.ERROR_INSUFFICIENT_BUFFER) {
		return "", fmt.Errorf("EvtFormatMessage sizing: %w", e)
	}
	if bufferUsed == 0 {
		return "", fmt.Errorf("EvtFormatMessage returned zero buffer size")
	}

	// BufferSize and BufferUsed count WCHARs, not bytes.
	buf := make([]uint16, bufferUsed)
	r, _, e = procEvtFormatMessage.Call(
		uintptr(publisherMetadata),
		uintptr(event),
		0, // MessageId
		0, // ValueCount
		0, // Values
		evtFormatMessageEvent,
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	if r == 0 {
		return "", fmt.Errorf("EvtFormatMessage render: %w", e)
	}
	return windows.UTF16ToString(buf), nil
}

// evtClose releases any EVT_HANDLE (subscription, event, bookmark,
// publisher metadata).
func evtClose(h windows.Handle) {
	if h != 0 {
		procEvtClose.Call(uintptr(h))
	}
}

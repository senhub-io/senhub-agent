//go:build windows

package windowseventlog

import (
	"context"
	"fmt"
	"sync"
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
// SCAFFOLD STATUS: written against the documented wevtapi contract and
// compiled for windows/amd64, but NOT yet validated against a live
// Windows Event Log (no Windows host in the dev loop). The runtime
// acceptance — bookmark survival, ~500 events/min under 1% CPU — is the
// Windows-CI gate tracked on issue #154.

// wevtapi entry points, resolved lazily on first use so the package
// links on any Windows SKU (the DLL ships with every supported Windows).
var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")

	procEvtSubscribe      = modwevtapi.NewProc("EvtSubscribe")
	procEvtNext           = modwevtapi.NewProc("EvtNext")
	procEvtRender         = modwevtapi.NewProc("EvtRender")
	procEvtClose          = modwevtapi.NewProc("EvtClose")
	procEvtCreateBookmark = modwevtapi.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark = modwevtapi.NewProc("EvtUpdateBookmark")
)

// EvtSubscribe flags (winevt.h EVT_SUBSCRIBE_FLAGS).
const (
	evtSubscribeToFutureEvents      = 1
	evtSubscribeStartAtOldestRecord = 2
	evtSubscribeStartAfterBookmark  = 3
)

// EvtRender flags (winevt.h EVT_RENDER_FLAGS).
const (
	evtRenderEventXml = 1
	evtRenderBookmark = 2
)

// drainBatchSize bounds one EvtNext call. 64 keeps the per-call render
// loop short so the stop event is observed promptly even on a busy
// channel.
const drainBatchSize = 64

// eventReader holds one subscription goroutine per configured channel,
// a shared stop event to unblock them, and the bookmark store they flush
// progress into.
type eventReader struct {
	cfg       WindowsEventLogProbeConfig
	log       *logger.ModuleLogger
	probeName string

	stopEvent windows.Handle
	bookmarks *bookmarkStore

	wg       sync.WaitGroup
	stopOnce sync.Once
}

// newEventReader opens a subscription per channel and starts draining.
// Returns an error only for unrecoverable setup failures (event creation,
// bookmark load); a single channel that fails to subscribe is logged and
// skipped so one bad channel name does not sink the whole probe.
func newEventReader(cfg WindowsEventLogProbeConfig, log *logger.ModuleLogger, probeName string) (*eventReader, error) {
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
		cfg:       cfg,
		log:       log,
		probeName: probeName,
		stopEvent: stop,
		bookmarks: store,
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

	signal, err := windows.CreateEvent(nil, 1 /*manual reset*/, 0, nil)
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

	waitHandles := []windows.Handle{signal, r.stopEvent}
	for {
		w, err := windows.WaitForMultipleObjects(waitHandles, false, windows.INFINITE)
		if err != nil {
			r.log.Error().Err(err).Str("channel", channel).Msg("wait failed; stopping channel")
			return
		}
		switch w {
		case windows.WAIT_OBJECT_0: // signal: events ready
			r.drain(channel, sub, bookmark)
			windows.ResetEvent(signal)
		case windows.WAIT_OBJECT_0 + 1: // stop event
			return
		default:
			r.log.Warn().Uint32("wait", w).Str("channel", channel).Msg("unexpected wait result; stopping channel")
			return
		}
	}
}

// drain reads every ready event from the subscription, rendering,
// filtering and publishing each, then flushes the advanced bookmark. It
// returns when EvtNext reports no more items.
func (r *eventReader) drain(channel string, sub, bookmark windows.Handle) {
	advanced := false
	for {
		events, err := evtNext(sub, drainBatchSize)
		if err != nil {
			// ERROR_NO_MORE_ITEMS is the normal end of a drain.
			if err != windows.ERROR_NO_MORE_ITEMS && err != windows.ERROR_TIMEOUT {
				r.log.Debug().Err(err).Str("channel", channel).Msg("EvtNext ended")
			}
			break
		}
		for _, ev := range events {
			if xmlStr, rerr := evtRenderXML(ev); rerr != nil {
				r.log.Debug().Err(rerr).Str("channel", channel).Msg("EvtRender failed; event skipped")
			} else if parsed, ok := parseEventXML(xmlStr); !ok {
				r.log.Debug().Str("channel", channel).Str("xml", truncate(xmlStr, 200)).
					Msg("Unparseable event XML; skipped")
			} else if r.cfg.shouldEmit(parsed) {
				agentstate.PublishLog(parsed.toLogRecord(r.probeName, r.cfg.RedactPII))
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
	if bmXML, perr := evtRenderBookmarkXML(bookmark); perr != nil {
		r.log.Debug().Err(perr).Str("channel", channel).Msg("render bookmark failed")
	} else {
		r.bookmarks.set(channel, bmXML)
		if werr := r.bookmarks.persist(); werr != nil {
			r.log.Warn().Err(werr).Str("channel", channel).Msg("persist bookmark failed")
		}
	}
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

// evtClose releases any EVT_HANDLE (subscription, event, bookmark).
func evtClose(h windows.Handle) {
	if h != 0 {
		procEvtClose.Call(uintptr(h))
	}
}

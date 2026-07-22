package agentstate

import (
	"sync"
	"sync/atomic"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Trace spans ride the agent as RAW OTLP proto (ResourceSpans batches).
// Unlike logs, spans have no agent-internal model: no component reads or
// rewrites them, so the OTLP receiver publishes the received proto
// verbatim and the OTLP export strategy forwards it unchanged
// (OTLP-in → OTLP-out). Keeping the proto end-to-end avoids a lossy
// convert/rebuild round-trip through a model nothing else would use.
//
// spanChannelState is the agent's single, process-lifetime span fan-out —
// the exact analogue of logChannelState, carrying batches instead of
// single records because a received ExportTraceServiceRequest is already
// batch-shaped and splitting it buys nothing.
type spanChannelState struct {
	mu      sync.RWMutex
	subs    []chan []*tracepb.ResourceSpans
	dropped atomic.Uint64
}

var spanCh = &spanChannelState{}

// SubscribeSpans returns a channel that will receive ResourceSpans
// batches published via PublishSpans. The buf parameter sets the receive
// buffer size; if the consumer falls behind enough to fill the buffer,
// batches are dropped (oldest-first) and the global drop counter is
// incremented — readable via GetDroppedSpanBatchesTotal.
//
// Callers must drain the channel; abandoned subscriptions waste memory
// until UnsubscribeSpans is called.
func SubscribeSpans(buf int) <-chan []*tracepb.ResourceSpans {
	if buf <= 0 {
		buf = 1024
	}
	ch := make(chan []*tracepb.ResourceSpans, buf)
	spanCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]chan []*tracepb.ResourceSpans, len(spanCh.subs), len(spanCh.subs)+1)
	copy(next, spanCh.subs)
	spanCh.subs = append(next, ch)
	spanCh.mu.Unlock()
	return ch
}

// UnsubscribeSpans disconnects a previously-subscribed channel. The
// channel is NOT closed: PublishSpans snapshots the subscriber list
// under RLock and sends after releasing it, so a close here could
// interleave into a send-on-closed-channel panic (#262). Consumers exit
// via their own context (the OTLP spans relay cancels before
// unsubscribing); the orphaned channel is garbage-collected.
func UnsubscribeSpans(ch <-chan []*tracepb.ResourceSpans) {
	spanCh.mu.Lock()
	defer spanCh.mu.Unlock()
	for i, sub := range spanCh.subs {
		// Compare by pointer through the receive-only conversion.
		if (<-chan []*tracepb.ResourceSpans)(sub) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]chan []*tracepb.ResourceSpans, 0, len(spanCh.subs)-1)
			next = append(next, spanCh.subs[:i]...)
			next = append(next, spanCh.subs[i+1:]...)
			spanCh.subs = next
			return
		}
	}
}

// PublishSpans fans out a raw ResourceSpans batch to every subscriber.
// Non-blocking: if any subscriber's buffer is full, the batch is dropped
// FOR THAT SUBSCRIBER ONLY (others still receive it). Drop count is
// bumped once per dropped batch per subscriber. Producers never wait —
// span relay is best-effort under backpressure.
//
// Drop-oldest semantics on a full buffer: we make one attempt to receive
// a stale batch off the channel before sending the new one. This keeps
// the channel reflecting the most recent activity rather than freezing
// on the oldest backlog.
func PublishSpans(rs []*tracepb.ResourceSpans) {
	if len(rs) == 0 {
		return
	}
	spanCh.mu.RLock()
	subs := spanCh.subs
	spanCh.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- rs:
			// Sent.
		default:
			// Full — drop one stale batch and try again. If we still
			// can't send, count as dropped (the new batch this time).
			select {
			case <-ch:
				spanCh.dropped.Add(1)
			default:
			}
			select {
			case ch <- rs:
			default:
				spanCh.dropped.Add(1)
			}
		}
	}
}

// GetDroppedSpanBatchesTotal returns the lifetime count of span batches
// dropped due to subscriber backpressure.
func GetDroppedSpanBatchesTotal() uint64 {
	return spanCh.dropped.Load()
}

// SpanSubscriberCount returns the number of active span subscribers.
// Zero means no trace-capable strategy is draining the channel, so
// PublishSpans would fan out to nobody. The OTLP receiver reads this to
// warn rather than silently discard ingested spans when no OTLP export
// strategy has signals.traces enabled.
func SpanSubscriberCount() int {
	spanCh.mu.RLock()
	defer spanCh.mu.RUnlock()
	return len(spanCh.subs)
}

// resetSpanChannelForTest clears all subscribers and resets the drop
// counter. Test-only helper to keep the package-level state from
// leaking across test cases.
func resetSpanChannelForTest() {
	spanCh.mu.Lock()
	spanCh.subs = nil
	spanCh.dropped.Store(0)
	spanCh.mu.Unlock()
}

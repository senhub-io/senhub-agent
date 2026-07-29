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
// spanSubscription binds a delivery channel to the strategy that owns it,
// the exact analogue of logSubscription. An empty strategy is a catch-all
// (SubscribeSpans); a named subscriber (SubscribeSpansFor) only receives
// broadcast batches and batches whose target list names it. Routing is
// decided once, at the single fan-out point — the trace analog of the
// metric router in data_store.go and of the log rail (#294 B).
type spanSubscription struct {
	strategy string
	ch       chan []*tracepb.ResourceSpans
}

// spanChannelState is the agent's single, process-lifetime span fan-out —
// the exact analogue of logChannelState, carrying batches instead of
// single records because a received ExportTraceServiceRequest is already
// batch-shaped and splitting it buys nothing.
type spanChannelState struct {
	mu      sync.RWMutex
	subs    []*spanSubscription
	dropped atomic.Uint64
}

var spanCh = &spanChannelState{}

// SubscribeSpans returns a catch-all channel that receives EVERY
// ResourceSpans batch published, regardless of routing target — the
// pre-#294 semantics. Trace-capable strategy relays that must honor
// endpoints: routing use SubscribeSpansFor instead.
//
// The buf parameter sets the receive buffer size; if the consumer falls
// behind enough to fill the buffer, batches are dropped (oldest-first)
// and the global drop counter is incremented — readable via
// GetDroppedSpanBatchesTotal.
//
// Callers must drain the channel; abandoned subscriptions waste memory
// until UnsubscribeSpans is called.
func SubscribeSpans(buf int) <-chan []*tracepb.ResourceSpans {
	return SubscribeSpansFor("", buf)
}

// SubscribeSpansFor returns a channel that receives span batches routed to
// the named strategy: broadcast batches (PublishSpans, or PublishSpansTo
// with empty targets) plus batches whose targets contain strategy. This is
// the routing-aware subscription that extends the metric/log endpoints:
// filter to traces (#294 B). An empty strategy behaves as a catch-all.
//
// Buffer, drop and drain semantics are identical to SubscribeSpans.
func SubscribeSpansFor(strategy string, buf int) <-chan []*tracepb.ResourceSpans {
	if buf <= 0 {
		buf = 1024
	}
	sub := &spanSubscription{strategy: strategy, ch: make(chan []*tracepb.ResourceSpans, buf)}
	spanCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]*spanSubscription, len(spanCh.subs), len(spanCh.subs)+1)
	copy(next, spanCh.subs)
	spanCh.subs = append(next, sub)
	spanCh.mu.Unlock()
	return sub.ch
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
		if (<-chan []*tracepb.ResourceSpans)(sub.ch) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]*spanSubscription, 0, len(spanCh.subs)-1)
			next = append(next, spanCh.subs[:i]...)
			next = append(next, spanCh.subs[i+1:]...)
			spanCh.subs = next
			return
		}
	}
}

// PublishSpans fans out a raw ResourceSpans batch to every subscriber
// (broadcast). Equivalent to PublishSpansTo with no targets; kept as the
// back-compatible entry point for producers that don't route.
func PublishSpans(rs []*tracepb.ResourceSpans) {
	PublishSpansTo(rs, nil)
}

// PublishSpansTo fans out a raw ResourceSpans batch to the subscribers it
// is routed to. Routing (#294 B): a named subscriber receives the batch
// only when targets is empty (broadcast) or contains its strategy name;
// catch-all subscribers always receive it. Non-routed subscribers are
// skipped entirely — no send, no drop count.
//
// Non-blocking: if a routed subscriber's buffer is full, the batch is
// dropped FOR THAT SUBSCRIBER ONLY (others still receive it). Drop count
// is bumped once per dropped batch per subscriber. Producers never wait —
// span relay is best-effort under backpressure.
//
// Drop-oldest semantics on a full buffer: we make one attempt to receive
// a stale batch off the channel before sending the new one. This keeps
// the channel reflecting the most recent activity rather than freezing
// on the oldest backlog.
func PublishSpansTo(rs []*tracepb.ResourceSpans, targets []string) {
	if len(rs) == 0 {
		return
	}
	spanCh.mu.RLock()
	subs := spanCh.subs
	spanCh.mu.RUnlock()
	for _, sub := range subs {
		if !recordRoutesTo(targets, sub.strategy) {
			continue
		}
		ch := sub.ch
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

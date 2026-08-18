package agentstate

import (
	"sync"
	"sync/atomic"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Logs INGESTED from a third-party application ride the agent as RAW OTLP
// proto (ResourceLogs batches), on this channel, separately from the
// agent's own log rail (log_channel.go, flattened LogRecord).
//
// The two models answer different needs and both are needed. A record the
// AGENT produces has no Resource of its own — it belongs to this process,
// so the flattened form plus the agent's Resource is exactly right. A
// record an APPLICATION sent already carries its own Resource, and
// re-emitting it through the agent's log provider would replace that
// identity with the agent's: the emitting app's service.name would be
// overwritten and applications would become indistinguishable downstream.
// Keeping the proto end-to-end preserves the emitter, matching what the
// span relay already guarantees for traces.
//
// logBatchSubscription binds a delivery channel to the strategy that owns
// it, the exact analogue of spanSubscription. An empty strategy is a
// catch-all (SubscribeLogBatches); a named subscriber
// (SubscribeLogBatchesFor) only receives broadcast batches and batches
// whose target list names it.
type logBatchSubscription struct {
	strategy string
	ch       chan []*logspb.ResourceLogs
}

// logBatchChannelState is the agent's single, process-lifetime fan-out for
// verbatim log batches — the exact analogue of spanChannelState, carrying
// batches because a received ExportLogsServiceRequest is already
// batch-shaped and splitting it would lose the Resource grouping that is
// the whole point of this path.
type logBatchChannelState struct {
	mu      sync.RWMutex
	subs    []*logBatchSubscription
	dropped atomic.Uint64
}

var logBatchCh = &logBatchChannelState{}

// SubscribeLogBatches returns a catch-all channel receiving EVERY verbatim
// ResourceLogs batch published, regardless of routing target.
//
// The buf parameter sets the receive buffer size; if the consumer falls
// behind enough to fill the buffer, batches are dropped (oldest-first) and
// the global drop counter is incremented — readable via
// GetDroppedLogBatchesTotal.
//
// Callers must drain the channel; abandoned subscriptions waste memory
// until UnsubscribeLogBatches is called.
func SubscribeLogBatches(buf int) <-chan []*logspb.ResourceLogs {
	return SubscribeLogBatchesFor("", buf)
}

// SubscribeLogBatchesFor returns a channel receiving verbatim log batches
// routed to the named strategy: broadcast batches plus batches whose
// targets contain strategy. An empty strategy behaves as a catch-all.
//
// Buffer, drop and drain semantics are identical to SubscribeLogBatches.
func SubscribeLogBatchesFor(strategy string, buf int) <-chan []*logspb.ResourceLogs {
	if buf <= 0 {
		buf = 1024
	}
	sub := &logBatchSubscription{strategy: strategy, ch: make(chan []*logspb.ResourceLogs, buf)}
	logBatchCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock and
	// iterate after releasing — the backing array must therefore never be
	// mutated in place (#262).
	next := make([]*logBatchSubscription, len(logBatchCh.subs), len(logBatchCh.subs)+1)
	copy(next, logBatchCh.subs)
	logBatchCh.subs = append(next, sub)
	logBatchCh.mu.Unlock()
	return sub.ch
}

// UnsubscribeLogBatches disconnects a previously-subscribed channel. The
// channel is NOT closed: publishers snapshot the subscriber list under
// RLock and send after releasing it, so a close here could interleave into
// a send-on-closed-channel panic (#262). Consumers exit via their own
// context; the orphaned channel is garbage-collected.
func UnsubscribeLogBatches(ch <-chan []*logspb.ResourceLogs) {
	logBatchCh.mu.Lock()
	defer logBatchCh.mu.Unlock()
	for i, sub := range logBatchCh.subs {
		if (<-chan []*logspb.ResourceLogs)(sub.ch) == ch {
			next := make([]*logBatchSubscription, 0, len(logBatchCh.subs)-1)
			next = append(next, logBatchCh.subs[:i]...)
			next = append(next, logBatchCh.subs[i+1:]...)
			logBatchCh.subs = next
			return
		}
	}
}

// PublishLogBatches fans out a raw ResourceLogs batch to every subscriber
// (broadcast). Equivalent to PublishLogBatchesTo with no targets.
func PublishLogBatches(rl []*logspb.ResourceLogs) {
	PublishLogBatchesTo(rl, nil)
}

// PublishLogBatchesTo fans out a raw ResourceLogs batch to the subscribers
// it is routed to, with the same routing, non-blocking and drop-oldest
// semantics as PublishSpansTo. Producers never wait — verbatim log relay
// is best-effort under backpressure, exactly like the span relay.
func PublishLogBatchesTo(rl []*logspb.ResourceLogs, targets []string) {
	if len(rl) == 0 {
		return
	}
	logBatchCh.mu.RLock()
	subs := logBatchCh.subs
	logBatchCh.mu.RUnlock()
	for _, sub := range subs {
		if !recordRoutesTo(targets, sub.strategy) {
			continue
		}
		ch := sub.ch
		select {
		case ch <- rl:
			// Sent.
		default:
			// Full — drop one stale batch and try again. If we still can't
			// send, count as dropped (the new batch this time).
			select {
			case <-ch:
				logBatchCh.dropped.Add(1)
			default:
			}
			select {
			case ch <- rl:
			default:
				logBatchCh.dropped.Add(1)
			}
		}
	}
}

// GetDroppedLogBatchesTotal returns the lifetime count of verbatim log
// batches dropped due to subscriber backpressure.
func GetDroppedLogBatchesTotal() uint64 {
	return logBatchCh.dropped.Load()
}

// LogBatchSubscriberCount returns the number of active verbatim-log
// subscribers. Zero means no strategy is draining the channel, so a
// published batch would fan out to nobody. The OTLP receiver reads this to
// decide whether the verbatim path is available for ingested logs.
func LogBatchSubscriberCount() int {
	logBatchCh.mu.RLock()
	defer logBatchCh.mu.RUnlock()
	return len(logBatchCh.subs)
}

// resetLogBatchChannelForTest clears all subscribers and resets the drop
// counter. Test-only helper to keep the package-level state from leaking
// across test cases.
func resetLogBatchChannelForTest() {
	logBatchCh.mu.Lock()
	logBatchCh.subs = nil
	logBatchCh.dropped.Store(0)
	logBatchCh.mu.Unlock()
}

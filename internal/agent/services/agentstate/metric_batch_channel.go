package agentstate

import (
	"sync"
	"sync/atomic"

	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// Metrics INGESTED from a third-party application ride the agent as RAW
// OTLP proto (ResourceMetrics batches) on this channel, IN ADDITION to the
// flattened DataPoint path that feeds the DataStore.
//
// Both are needed, and they serve different sinks. PRTG, Nagios,
// Prometheus, the web UI and the cloud sink read the flattened model from
// the cache and have no notion of an OTLP Resource — for them nothing
// changes, and the emitter's resource attributes keep arriving as tags.
// The OTLP output is the only place a Resource exists, and there
// re-encoding an application's datapoints under the AGENT's Resource
// leaves two different values for a reserved identity key (service.name)
// in one export, which a backend resolves by silently keeping one. The
// verbatim batch removes the ambiguity by shipping the emitter's Resource
// as sent, matching what the span and log relays already guarantee.
//
// metricBatchSubscription binds a delivery channel to the strategy that owns
// it, the exact analogue of spanSubscription and logBatchSubscription. An empty strategy is a
// catch-all (SubscribeMetricBatches); a named subscriber
// (SubscribeMetricBatchesFor) only receives broadcast batches and batches
// whose target list names it.
type metricBatchSubscription struct {
	strategy string
	ch       chan []*metricpb.ResourceMetrics
}

// metricBatchChannelState is the agent's single, process-lifetime fan-out
// for verbatim metric batches — the exact analogue of spanChannelState,
// carrying batches because a received ExportMetricsServiceRequest is
// already batch-shaped and splitting it would lose the Resource grouping
// that is the whole point of this path.
type metricBatchChannelState struct {
	mu      sync.RWMutex
	subs    []*metricBatchSubscription
	dropped atomic.Uint64
}

var metricBatchCh = &metricBatchChannelState{}

// SubscribeMetricBatches returns a catch-all channel receiving EVERY verbatim
// ResourceLogs batch published, regardless of routing target.
//
// The buf parameter sets the receive buffer size; if the consumer falls
// behind enough to fill the buffer, batches are dropped (oldest-first) and
// the global drop counter is incremented — readable via
// GetDroppedMetricBatchesTotal.
//
// Callers must drain the channel; abandoned subscriptions waste memory
// until UnsubscribeMetricBatches is called.
func SubscribeMetricBatches(buf int) <-chan []*metricpb.ResourceMetrics {
	return SubscribeMetricBatchesFor("", buf)
}

// SubscribeMetricBatchesFor returns a channel receiving verbatim metric batches
// routed to the named strategy: broadcast batches plus batches whose
// targets contain strategy. An empty strategy behaves as a catch-all.
//
// Buffer, drop and drain semantics are identical to SubscribeMetricBatches.
func SubscribeMetricBatchesFor(strategy string, buf int) <-chan []*metricpb.ResourceMetrics {
	if buf <= 0 {
		buf = 1024
	}
	sub := &metricBatchSubscription{strategy: strategy, ch: make(chan []*metricpb.ResourceMetrics, buf)}
	metricBatchCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock and
	// iterate after releasing — the backing array must therefore never be
	// mutated in place (#262).
	next := make([]*metricBatchSubscription, len(metricBatchCh.subs), len(metricBatchCh.subs)+1)
	copy(next, metricBatchCh.subs)
	metricBatchCh.subs = append(next, sub)
	metricBatchCh.mu.Unlock()
	return sub.ch
}

// UnsubscribeMetricBatches disconnects a previously-subscribed channel. The
// channel is NOT closed: publishers snapshot the subscriber list under
// RLock and send after releasing it, so a close here could interleave into
// a send-on-closed-channel panic (#262). Consumers exit via their own
// context; the orphaned channel is garbage-collected.
func UnsubscribeMetricBatches(ch <-chan []*metricpb.ResourceMetrics) {
	metricBatchCh.mu.Lock()
	defer metricBatchCh.mu.Unlock()
	for i, sub := range metricBatchCh.subs {
		if (<-chan []*metricpb.ResourceMetrics)(sub.ch) == ch {
			next := make([]*metricBatchSubscription, 0, len(metricBatchCh.subs)-1)
			next = append(next, metricBatchCh.subs[:i]...)
			next = append(next, metricBatchCh.subs[i+1:]...)
			metricBatchCh.subs = next
			return
		}
	}
}

// PublishMetricBatches fans out a raw ResourceLogs batch to every subscriber
// (broadcast). Equivalent to PublishMetricBatchesTo with no targets.
func PublishMetricBatches(rl []*metricpb.ResourceMetrics) {
	PublishMetricBatchesTo(rl, nil)
}

// PublishMetricBatchesTo fans out a raw ResourceLogs batch to the subscribers
// it is routed to, with the same routing, non-blocking and drop-oldest
// semantics as PublishSpansTo. Producers never wait — verbatim metric relay
// is best-effort under backpressure, exactly like the span relay.
func PublishMetricBatchesTo(rl []*metricpb.ResourceMetrics, targets []string) {
	if len(rl) == 0 {
		return
	}
	metricBatchCh.mu.RLock()
	subs := metricBatchCh.subs
	metricBatchCh.mu.RUnlock()
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
				metricBatchCh.dropped.Add(1)
			default:
			}
			select {
			case ch <- rl:
			default:
				metricBatchCh.dropped.Add(1)
			}
		}
	}
}

// GetDroppedMetricBatchesTotal returns the lifetime count of verbatim log
// batches dropped due to subscriber backpressure.
func GetDroppedMetricBatchesTotal() uint64 {
	return metricBatchCh.dropped.Load()
}

// MetricBatchSubscriberCount returns the number of active verbatim-log
// subscribers. Zero means no strategy is draining the channel, so a
// published batch would fan out to nobody. The OTLP receiver reads this to
// decide whether the verbatim path is available for ingested logs.
func MetricBatchSubscriberCount() int {
	metricBatchCh.mu.RLock()
	defer metricBatchCh.mu.RUnlock()
	return len(metricBatchCh.subs)
}

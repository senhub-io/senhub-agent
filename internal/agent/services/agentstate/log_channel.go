package agentstate

import (
	"sync"
	"sync/atomic"
	"time"
)

// LogRecord is the agent-internal log envelope. Producers (syslog
// probe, event probe, future os-log probes) populate it; consumers
// (OTLP strategy today, potentially others tomorrow) translate it
// into the wire format they speak.
//
// Field semantics deliberately mirror the OTel log data model so the
// translation to OTel is mechanical:
//   - Severity / SeverityText follow the OTel severity table (RFC 5424
//     mapping where applicable; producers compute the mapping)
//   - Body is the human-readable message content
//   - Attributes carry structured fields (syslog facility, hostname,
//     event payload keys, …) — names use OTel dotted form
//   - Resource overrides are *additional* resource attrs that should be
//     attached only to records from this producer (e.g. "syslog.client"
//     IP for a syslog server probe). Strategy resource attrs always win
//     for keys that overlap.
//
// We do NOT depend on go.opentelemetry.io/otel/log here so that probes
// publishing logs don't pull in the SDK. The OTLP strategy converts on
// the consumer side.
type LogRecord struct {
	Timestamp    time.Time
	Severity     LogSeverity
	SeverityText string
	Body         string
	Attributes   map[string]string

	// ProducerProbeName / ProducerProbeType identify the probe that
	// produced this record. Used by the strategy to populate the
	// service.instance.id-equivalent attribute on the OTel side, and
	// by self-metrics to attribute drops by source.
	ProducerProbeName string
	ProducerProbeType string

	// TargetStrategies routes this record to specific log-capable
	// strategies, mirroring the metric router (data_store.go: a
	// datapoint reaches a strategy only when the probe's target list
	// names it). A record is delivered to a routing-aware subscriber
	// (SubscribeLogsFor) only when this list contains that subscriber's
	// strategy name. An EMPTY (or nil) list means "broadcast": the
	// record goes to every subscriber. Broadcast is the pre-#294
	// behavior and the default for producers that don't opt into
	// routing, so leaving it empty is fully backward-compatible.
	//
	// Producers stamp this from their probe's target-strategy resolution
	// so the endpoints:/strategy filtering that governs metrics applies
	// to logs too (#294 rail B). Catch-all subscribers (SubscribeLogs,
	// empty strategy name) receive every record regardless of this list.
	TargetStrategies []string
}

// LogSeverity mirrors the OTel SeverityNumber range (1..24). Producers
// fill in the value matching the standard mapping table; the OTLP
// strategy converts to go.opentelemetry.io/otel/log.Severity 1:1.
//
// Defined locally rather than re-exported from the OTel SDK so that
// log producers (probes) don't pick up the SDK as a transitive dep.
type LogSeverity uint8

// Standard OTel log severity values. Names match the spec exactly.
// Numeric values are stable across OTel releases.
const (
	LogSeverityUnspecified LogSeverity = 0
	LogSeverityTrace       LogSeverity = 1
	LogSeverityDebug       LogSeverity = 5
	LogSeverityInfo        LogSeverity = 9
	LogSeverityWarn        LogSeverity = 13
	LogSeverityError       LogSeverity = 17
	LogSeverityFatal       LogSeverity = 21
)

// SyslogPriorityToSeverity maps RFC 5424 PRI severity values (0..7) to
// OTel SeverityNumber per the OTel Logs Data Model §4.2 table.
//
//	0 emergency → FATAL4 (24)
//	1 alert     → FATAL3 (23)
//	2 critical  → FATAL2 (22)
//	3 error     → ERROR  (17)
//	4 warning   → WARN   (13)
//	5 notice    → INFO2  (10)
//	6 info      → INFO   (9)
//	7 debug     → DEBUG  (5)
//
// Out-of-range inputs return Unspecified rather than panicking — keeps
// the path resilient to malformed syslog messages.
func SyslogPriorityToSeverity(pri int) LogSeverity {
	switch pri {
	case 0:
		return 24
	case 1:
		return 23
	case 2:
		return 22
	case 3:
		return LogSeverityError
	case 4:
		return LogSeverityWarn
	case 5:
		return 10
	case 6:
		return LogSeverityInfo
	case 7:
		return LogSeverityDebug
	}
	return LogSeverityUnspecified
}

// SyslogPriorityToText returns the standard OTel SeverityText for the
// same mapping. Empty string for out-of-range inputs.
func SyslogPriorityToText(pri int) string {
	switch pri {
	case 0:
		return "FATAL4"
	case 1:
		return "FATAL3"
	case 2:
		return "FATAL2"
	case 3:
		return "ERROR"
	case 4:
		return "WARN"
	case 5:
		return "INFO2"
	case 6:
		return "INFO"
	case 7:
		return "DEBUG"
	}
	return ""
}

// logSubscription binds a delivery channel to the strategy that owns
// it. An empty strategy marks a catch-all subscriber (SubscribeLogs)
// that receives every record; a named subscriber (SubscribeLogsFor)
// only receives broadcast records and records whose TargetStrategies
// name it — this is where log routing is decided, once, at the single
// fan-out point (the log analog of the metric router in data_store.go).
type logSubscription struct {
	strategy string
	ch       chan LogRecord
}

// logChannelState is the agent's single, process-lifetime log fan-out.
// One producer (any probe), one or more consumers (the OTLP strategy
// today; additional log-capable strategies tomorrow). Stored as a
// package var because there's only one of it and exposing a constructor
// would require all probes to thread a handle through their
// constructors — friction with no upside.
type logChannelState struct {
	mu      sync.RWMutex
	subs    []*logSubscription
	dropped atomic.Uint64
}

var logCh = &logChannelState{}

// SubscribeLogs returns a catch-all channel that receives EVERY log
// record published via PublishLog, regardless of the record's
// TargetStrategies routing. Use it for taps and consumers that are not
// tied to a routed strategy (the pre-#294 semantics). Strategy pumps
// that must honor endpoints: routing use SubscribeLogsFor instead.
//
// The buf parameter sets the receive buffer size; if the consumer falls
// behind enough to fill the buffer, records are dropped (oldest-first)
// and the global drop counter is incremented — readable via
// GetDroppedLogRecordsTotal.
//
// Callers must drain the channel; abandoned subscriptions waste a
// goroutine until UnsubscribeLogs is called.
func SubscribeLogs(buf int) <-chan LogRecord {
	return SubscribeLogsFor("", buf)
}

// SubscribeLogsFor returns a channel that receives log records routed to
// the named strategy: broadcast records (empty TargetStrategies) plus
// records whose TargetStrategies contains strategy. This is the
// routing-aware subscription that makes the endpoints:/strategy filter
// apply to logs the same way it applies to metrics (#294 rail B). An
// empty strategy behaves as a catch-all (see SubscribeLogs).
//
// Buffer, drop and drain semantics are identical to SubscribeLogs.
func SubscribeLogsFor(strategy string, buf int) <-chan LogRecord {
	if buf <= 0 {
		buf = 1024
	}
	sub := &logSubscription{strategy: strategy, ch: make(chan LogRecord, buf)}
	logCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]*logSubscription, len(logCh.subs), len(logCh.subs)+1)
	copy(next, logCh.subs)
	logCh.subs = append(next, sub)
	logCh.mu.Unlock()
	return sub.ch
}

// UnsubscribeLogs disconnects a previously-subscribed channel. The
// channel is NOT closed: PublishLog snapshots the subscriber list
// under RLock and sends after releasing it, so a close here could
// interleave into a send-on-closed-channel panic (#262). Consumers
// exit via their own context (the OTLP logs pump cancels before
// unsubscribing); the orphaned channel is garbage-collected.
func UnsubscribeLogs(ch <-chan LogRecord) {
	logCh.mu.Lock()
	defer logCh.mu.Unlock()
	for i, sub := range logCh.subs {
		// Compare by pointer through the receive-only conversion.
		if (<-chan LogRecord)(sub.ch) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]*logSubscription, 0, len(logCh.subs)-1)
			next = append(next, logCh.subs[:i]...)
			next = append(next, logCh.subs[i+1:]...)
			logCh.subs = next
			return
		}
	}
}

// PublishLog fans out a record to every subscriber it is routed to.
// Routing (#294 rail B): a named subscriber receives the record only
// when rec.TargetStrategies is empty (broadcast) or contains that
// subscriber's strategy name; catch-all subscribers always receive it.
// Non-routed subscribers are skipped entirely — no send, no drop count.
//
// Non-blocking: if a routed subscriber's buffer is full, the record is
// dropped FOR THAT SUBSCRIBER ONLY (others still receive it). Drop count
// is bumped once per dropped record per subscriber. Producers should
// never wait — log emission is best-effort under backpressure.
//
// Drop-oldest semantics on a full buffer: we make one attempt to
// receive a stale record off the channel before sending the new one.
// This keeps the channel reflecting the most recent activity rather
// than freezing on the oldest backlog.
func PublishLog(rec LogRecord) {
	rec.Attributes = enrichLogAttributes(rec.Attributes, rec.ProducerProbeName)
	logCh.mu.RLock()
	subs := logCh.subs
	logCh.mu.RUnlock()
	for _, sub := range subs {
		if !recordRoutesTo(rec.TargetStrategies, sub.strategy) {
			continue
		}
		ch := sub.ch
		select {
		case ch <- rec:
			// Sent.
		default:
			// Full — drop one stale record and try again. If we still
			// can't send, count as dropped (the new record this time).
			select {
			case <-ch:
				logCh.dropped.Add(1)
			default:
			}
			select {
			case ch <- rec:
			default:
				logCh.dropped.Add(1)
			}
		}
	}
}

// enrichLogAttributes overlays the producer probe's operator-configured
// custom_tags onto a log record's attributes for cross-signal correlation
// (#294): the metric router already applies custom_tags to datapoints, but
// logs never passed through it. Operator tags win on a key conflict, the
// same precedence the metric enrichment uses (custom_tags > built-in).
//
// Returns the original map untouched (no allocation) when the producer has
// no configured custom_tags — the overwhelmingly common path. Otherwise it
// builds a fresh merged map, never mutating the caller's attributes (the
// same record value fans out to every subscriber; a shared-map mutation
// would race, cf. the copy-on-write subscriber-list invariant #262).
func enrichLogAttributes(attrs map[string]string, probeName string) map[string]string {
	custom := customTagsForProbe(probeName)
	if len(custom) == 0 {
		return attrs
	}
	merged := make(map[string]string, len(attrs)+len(custom))
	for k, v := range attrs {
		merged[k] = v
	}
	for k, v := range custom {
		merged[k] = v
	}
	return merged
}

// recordRoutesTo reports whether a record with the given TargetStrategies
// should be delivered to a subscriber owned by strategy. A catch-all
// subscriber (empty strategy) always matches; an empty target list is a
// broadcast that matches every subscriber; otherwise the target list
// must name the subscriber's strategy.
func recordRoutesTo(targets []string, strategy string) bool {
	if strategy == "" || len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		if t == strategy {
			return true
		}
	}
	return false
}

// GetDroppedLogRecordsTotal returns the lifetime count of log records
// dropped due to subscriber backpressure. Used by the OTLP strategy's
// self-observability metric.
func GetDroppedLogRecordsTotal() uint64 {
	return logCh.dropped.Load()
}

// LogSubscriberCount returns the number of active log subscribers. Zero
// means no log-capable strategy is draining the channel, so PublishLog
// would fan out to nobody. The OTLP receiver reads this to warn rather
// than silently discard ingested logs when no OTLP export is configured.
func LogSubscriberCount() int {
	logCh.mu.RLock()
	defer logCh.mu.RUnlock()
	return len(logCh.subs)
}

// resetLogChannelForTest clears all subscribers and resets the drop
// counter. Test-only helper to keep the package-level state from
// leaking across test cases.
func resetLogChannelForTest() {
	logCh.mu.Lock()
	logCh.subs = nil
	logCh.dropped.Store(0)
	logCh.mu.Unlock()
}

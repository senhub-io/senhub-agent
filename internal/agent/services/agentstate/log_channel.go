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

// logChannelState is the agent's single, process-lifetime log fan-out.
// One producer (any probe), one consumer (currently the OTLP strategy;
// could be more later). Stored as a package var because there's only
// one of it and exposing a constructor would require all probes to
// thread a handle through their constructors — friction with no upside.
type logChannelState struct {
	mu      sync.RWMutex
	subs    []chan LogRecord
	dropped atomic.Uint64
}

var logCh = &logChannelState{}

// SubscribeLogs returns a channel that will receive log records
// published via PublishLog. The buf parameter sets the receive buffer
// size; if the consumer falls behind enough to fill the buffer,
// records are dropped (oldest-first) and the global drop counter is
// incremented — readable via GetDroppedLogRecordsTotal.
//
// Callers must drain the channel; abandoned subscriptions waste a
// goroutine until UnsubscribeLogs is called.
func SubscribeLogs(buf int) <-chan LogRecord {
	if buf <= 0 {
		buf = 1024
	}
	ch := make(chan LogRecord, buf)
	logCh.mu.Lock()
	// Copy-on-write: publishers snapshot the slice header under RLock
	// and iterate after releasing — the backing array must therefore
	// never be mutated in place (#262).
	next := make([]chan LogRecord, len(logCh.subs), len(logCh.subs)+1)
	copy(next, logCh.subs)
	logCh.subs = append(next, ch)
	logCh.mu.Unlock()
	return ch
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
		if (<-chan LogRecord)(sub) == ch {
			// Copy-on-write removal — never shift the shared backing
			// array in place (#262).
			next := make([]chan LogRecord, 0, len(logCh.subs)-1)
			next = append(next, logCh.subs[:i]...)
			next = append(next, logCh.subs[i+1:]...)
			logCh.subs = next
			return
		}
	}
}

// PublishLog fans out a record to every subscriber. Non-blocking: if
// any subscriber's buffer is full, the record is dropped FOR THAT
// SUBSCRIBER ONLY (others still receive it). Drop count is bumped once
// per dropped record per subscriber. Producers should never wait —
// log emission is best-effort under backpressure.
//
// Drop-oldest semantics on a full buffer: we make one attempt to
// receive a stale record off the channel before sending the new one.
// This keeps the channel reflecting the most recent activity rather
// than freezing on the oldest backlog.
func PublishLog(rec LogRecord) {
	logCh.mu.RLock()
	subs := logCh.subs
	logCh.mu.RUnlock()
	for _, ch := range subs {
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

// GetDroppedLogRecordsTotal returns the lifetime count of log records
// dropped due to subscriber backpressure. Used by the OTLP strategy's
// self-observability metric.
func GetDroppedLogRecordsTotal() uint64 {
	return logCh.dropped.Load()
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

package otlpreceiver

import (
	"encoding/hex"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// flattenResourceLogs converts the OTLP wire form of a logs payload into the
// agent's internal LogRecord envelopes. Resource attributes are folded onto
// every record's attributes (record attributes win on a key collision), so a
// downstream sink keeps host.name / service.* even though the agent has no
// per-process Resource for an ingested external stream. The OTLP severity
// number maps 1:1 onto agentstate.LogSeverity (the same OTel 1..24 scale).
func flattenResourceLogs(resourceLogs []*logspb.ResourceLogs, probeName string) []agentstate.LogRecord {
	var out []agentstate.LogRecord
	for _, rl := range resourceLogs {
		resourceAttrs := attributesToMap(rl.GetResource().GetAttributes())
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				out = append(out, logRecordToInternal(lr, resourceAttrs, probeName))
			}
		}
	}
	return out
}

func logRecordToInternal(lr *logspb.LogRecord, resourceAttrs map[string]string, probeName string) agentstate.LogRecord {
	// Prefer the event time; fall back to the collector's observed time,
	// then to now for a sender that stamped neither.
	tsNano := lr.GetTimeUnixNano()
	if tsNano == 0 {
		tsNano = lr.GetObservedTimeUnixNano()
	}
	when := time.Now()
	if tsNano != 0 {
		when = time.Unix(0, int64(tsNano))
	}

	attrs := make(map[string]string, len(resourceAttrs)+len(lr.GetAttributes())+2)
	for k, v := range resourceAttrs {
		attrs[k] = v
	}
	for _, kv := range lr.GetAttributes() {
		if kv.GetKey() != "" {
			attrs[kv.GetKey()] = anyValueToString(kv.GetValue())
		}
	}
	// Preserve trace correlation when the record carries it.
	if tid := lr.GetTraceId(); len(tid) > 0 {
		attrs["trace_id"] = hex.EncodeToString(tid)
	}
	if sid := lr.GetSpanId(); len(sid) > 0 {
		attrs["span_id"] = hex.EncodeToString(sid)
	}

	return agentstate.LogRecord{
		Timestamp:         when,
		Severity:          clampSeverity(lr.GetSeverityNumber()),
		SeverityText:      lr.GetSeverityText(),
		Body:              anyValueToString(lr.GetBody()),
		Attributes:        attrs,
		ProducerProbeName: probeName,
		ProducerProbeType: probeType,
	}
}

// clampSeverity maps an OTLP SeverityNumber onto agentstate.LogSeverity.
// Both use the OTel 1..24 scale; out-of-range values (including
// UNSPECIFIED = 0) become Unspecified rather than a bogus severity.
func clampSeverity(n logspb.SeverityNumber) agentstate.LogSeverity {
	v := int32(n)
	if v < 1 || v > 24 {
		return agentstate.LogSeverityUnspecified
	}
	return agentstate.LogSeverity(v)
}

// attributesToMap coerces OTLP KeyValues into a string map (empty keys
// skipped), mirroring how the metrics decoder folds attributes to tags.
func attributesToMap(attrs []*commonpb.KeyValue) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv.GetKey() != "" {
			out[kv.GetKey()] = anyValueToString(kv.GetValue())
		}
	}
	return out
}

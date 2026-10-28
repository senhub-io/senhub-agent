package agentstate

import (
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// RedactLogBatches masks, in place, the body and the attributes of every
// record of a verbatim ResourceLogs batch with the rules the flat log rail
// applies in PublishLog. The verbatim path hands the received protobuf to
// the outputs untouched, so without this step it would carry the secrets
// the flat path masks. The batch must not be shared with another reader
// while this runs.
func RedactLogBatches(rl []*logspb.ResourceLogs, probe string) {
	if logRedactionDisabled(probe) {
		return
	}
	rs := loadRedactionRuleset()
	if len(rs.rules) == 0 {
		return
	}
	for _, r := range rl {
		for _, sl := range r.GetScopeLogs() {
			for _, rec := range sl.GetLogRecords() {
				redactAnyValue(rs, rec.GetBody(), probe)
				redactKeyValues(rs, rec.GetAttributes(), probe)
			}
		}
	}
}

func redactKeyValues(rs *redactionRuleset, kvs []*commonpb.KeyValue, probe string) {
	for _, kv := range kvs {
		sv, ok := kv.GetValue().GetValue().(*commonpb.AnyValue_StringValue)
		if !ok {
			redactAnyValue(rs, kv.GetValue(), probe)
			continue
		}
		if masked, keyed := maskByKey(rs, kv.GetKey(), sv.StringValue, probe); keyed {
			sv.StringValue = masked
			continue
		}
		sv.StringValue = maskString(rs, sv.StringValue, probe)
	}
}

func redactAnyValue(rs *redactionRuleset, v *commonpb.AnyValue, probe string) {
	switch t := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		t.StringValue = maskString(rs, t.StringValue, probe)
	case *commonpb.AnyValue_KvlistValue:
		redactKeyValues(rs, t.KvlistValue.GetValues(), probe)
	case *commonpb.AnyValue_ArrayValue:
		for _, e := range t.ArrayValue.GetValues() {
			redactAnyValue(rs, e, probe)
		}
	}
}

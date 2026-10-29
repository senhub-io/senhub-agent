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
	pemEnabled := hasRedactRule(rs, "private_key")
	for _, r := range rl {
		redactKeyValues(rs, r.GetResource().GetAttributes(), probe)
		for _, sl := range r.GetScopeLogs() {
			redactKeyValues(rs, sl.GetScope().GetAttributes(), probe)
			for _, rec := range sl.GetLogRecords() {
				if sv, ok := rec.GetBody().GetValue().(*commonpb.AnyValue_StringValue); ok {
					sv.StringValue = maskBatchBody(rs, sv.StringValue, rec.GetAttributes(), probe, pemEnabled)
				} else {
					redactAnyValue(rs, rec.GetBody(), probe)
				}
				redactKeyValues(rs, rec.GetAttributes(), probe)
			}
		}
	}
}

// maskBatchBody masks a record body, following a private key whose lines
// arrive as separate records with a tracker of its own so that the flat
// copy of the same records keeps its state apart.
func maskBatchBody(rs *redactionRuleset, body string, attrs []*commonpb.KeyValue, probe string, pemEnabled bool) string {
	rec := LogRecord{Body: body, ProducerProbeName: probe}
	if pemEnabled {
		for _, kv := range attrs {
			switch kv.GetKey() {
			case "log.file.path", "log.iostream", "systemd.unit", "container.name", "syslog.hostname", "syslog.appname":
				if rec.Attributes == nil {
					rec.Attributes = make(map[string]string, 2)
				}
				rec.Attributes[kv.GetKey()] = kv.GetValue().GetStringValue()
			}
		}
	}
	maskTail := func(s string) string { return maskString(rs, s, probe) }
	if masked, out := batchPemState.process(rec, pemEnabled, pemEnabled && hasUnclosedPEMBegin(body), maskTail); masked {
		return out
	}
	return maskString(rs, body, probe)
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

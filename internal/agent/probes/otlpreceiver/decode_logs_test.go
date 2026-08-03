package otlpreceiver

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func wrapLogs(resourceAttrs []*commonpb.KeyValue, records ...*logspb.LogRecord) []*logspb.ResourceLogs {
	return []*logspb.ResourceLogs{
		{
			Resource:  &resourcepb.Resource{Attributes: resourceAttrs},
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}},
		},
	}
}

func TestFlattenResourceLogs_MapsFields(t *testing.T) {
	rec := &logspb.LogRecord{
		TimeUnixNano:   1_700_000_000_000_000_000,
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
		SeverityText:   "ERROR",
		Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "disk full"}},
		Attributes:     []*commonpb.KeyValue{strAttr("log.file.name", "app.log")},
		TraceId:        []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanId:         []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
	}
	out := flattenResourceLogs(
		wrapLogs([]*commonpb.KeyValue{strAttr("host.name", "edge-01"), strAttr("service.name", "api")}, rec),
		"otlp_receiver_test",
	)
	if len(out) != 1 {
		t.Fatalf("got %d records, want 1", len(out))
	}
	got := out[0]

	if got.Severity != agentstate.LogSeverityError {
		t.Errorf("severity = %d, want %d (ERROR)", got.Severity, agentstate.LogSeverityError)
	}
	if got.SeverityText != "ERROR" {
		t.Errorf("severity text = %q, want ERROR", got.SeverityText)
	}
	if got.Body != "disk full" {
		t.Errorf("body = %q, want 'disk full'", got.Body)
	}
	if got.ProducerProbeName != "otlp_receiver_test" || got.ProducerProbeType != "otlp_receiver" {
		t.Errorf("producer = %q/%q", got.ProducerProbeName, got.ProducerProbeType)
	}
	if got.Timestamp.UnixNano() != 1_700_000_000_000_000_000 {
		t.Errorf("timestamp = %d, want 1_700_000_000_000_000_000", got.Timestamp.UnixNano())
	}
	// Resource attrs folded in alongside record attrs; trace correlation kept.
	for k, want := range map[string]string{
		"host.name":     "edge-01",
		"service.name":  "api",
		"log.file.name": "app.log",
		"trace_id":      "0102030405060708090a0b0c0d0e0f10",
		"span_id":       "1112131415161718",
	} {
		if got.Attributes[k] != want {
			t.Errorf("attr %q = %q, want %q", k, got.Attributes[k], want)
		}
	}
}

func TestFlattenResourceLogs_ObservedTimeFallback(t *testing.T) {
	rec := &logspb.LogRecord{
		ObservedTimeUnixNano: 1_700_000_000_000_000_001,
		SeverityNumber:       logspb.SeverityNumber_SEVERITY_NUMBER_INFO,
		Body:                 &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hi"}},
	}
	out := flattenResourceLogs(wrapLogs(nil, rec), "p")
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	if out[0].Timestamp.UnixNano() != 1_700_000_000_000_000_001 {
		t.Errorf("expected observed-time fallback, got %d", out[0].Timestamp.UnixNano())
	}
}

func TestClampSeverity(t *testing.T) {
	cases := []struct {
		in   logspb.SeverityNumber
		want agentstate.LogSeverity
	}{
		{logspb.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED, agentstate.LogSeverityUnspecified},
		{logspb.SeverityNumber_SEVERITY_NUMBER_INFO, agentstate.LogSeverityInfo},
		{logspb.SeverityNumber_SEVERITY_NUMBER_FATAL4, 24},
		{logspb.SeverityNumber(99), agentstate.LogSeverityUnspecified},
		{logspb.SeverityNumber(-1), agentstate.LogSeverityUnspecified},
	}
	for _, c := range cases {
		if got := clampSeverity(c.in); got != c.want {
			t.Errorf("clampSeverity(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

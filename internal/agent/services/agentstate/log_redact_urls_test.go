package agentstate

import (
	"reflect"
	"strings"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func TestRedactURLAndBearerRules(t *testing.T) {
	setupRedactionTest(t)
	cases := []struct{ name, in, want string }{
		{"userinfo", "dial https://admin:hunter2@db.example.com:5432/x failed", "dial https://admin:***@db.example.com:5432/x failed"},
		{"userinfo scheme with plus", "postgres+ssl://u:p%40ss@h/db", "postgres+ssl://u:***@h/db"},
		{"url without password", "GET https://example.com/a@b", "GET https://example.com/a@b"},
		{"user only", "ssh://git@github.com/org/repo", "ssh://git@github.com/org/repo"},
		{"query key", "GET /v1/map?key=AbC123&lang=fr", "GET /v1/map?key=***&lang=fr"},
		{"query key uppercase", "GET /v1/map?lang=fr&KEY=AbC123", "GET /v1/map?lang=fr&KEY=***"},
		{"query sig", "GET /f?x=1&sig=deadbeef HTTP/1.1", "GET /f?x=1&sig=*** HTTP/1.1"},
		{"key outside query", "cache key=abc evicted", "cache key=abc evicted"},
		{"monkey param", "GET /x?monkey=1", "GET /x?monkey=1"},
		{"bare bearer", "retry with Bearer abcdef0123456789ABCDEF", "retry with Bearer ***"},
		{"bearer prose", "the bearer of this message", "the bearer of this message"},
		{"bearer short", "bearer abc", "bearer abc"},
		{"refresh token", `{"refresh_token":"r-123"}`, `{"refresh_token":"***"}`},
		{"client secret", "client_secret=s3cr3t&grant_type=x", "client_secret=***&grant_type=x"},
		{"several", "https://a:b@h/p?key=k1&token=t1 password=p1", "https://a:***@h/p?key=***&token=*** password=***"},
		{"clean", `10.0.0.5 - - "GET /index.html HTTP/1.1" 200 512`, `10.0.0.5 - - "GET /index.html HTTP/1.1" 200 512`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "p"}).Body
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestRedactProbeOptOut(t *testing.T) {
	setupRedactionTest(t)
	t.Cleanup(resetSignalContextForTest)
	SetSignalContext(&SignalContext{LogRedactOffProbes: map[string]struct{}{"raw": {}}})

	in := "password=hunter2"
	if got := redactLogRecord(LogRecord{Body: in, ProducerProbeName: "raw"}).Body; got != in {
		t.Fatalf("opted-out probe masked: %q", got)
	}
	if got := redactLogRecord(LogRecord{Body: in, ProducerProbeName: "other"}).Body; got != "password=***" {
		t.Fatalf("other probe not masked: %q", got)
	}
	if n := len(LogRedactedCounts()); n != 1 {
		t.Fatalf("counters = %d, want 1 (the masked probe only)", n)
	}
}

func TestRedactLogBatches(t *testing.T) {
	setupRedactionTest(t)
	t.Cleanup(resetSignalContextForTest)
	str := func(s string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
	mk := func() []*logspb.ResourceLogs {
		return []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			Body: str("login passhash=123456789 ok"),
			Attributes: []*commonpb.KeyValue{
				{Key: "url.full", Value: str("https://u:pw@h/x")},
				{Key: "http.request.header.authorization", Value: str("Bearer abc")},
				{Key: "nested", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
					Values: []*commonpb.KeyValue{{Key: "msg", Value: str("token=zzz")}},
				}}}},
				{Key: "count", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 3}}},
				{Key: "clean", Value: str("nothing here")},
			},
		}}}}}}
	}

	rl := mk()
	RedactLogBatches(rl, "recv")
	rec := rl[0].ScopeLogs[0].LogRecords[0]
	if got := rec.Body.GetStringValue(); got != "login passhash=*** ok" {
		t.Fatalf("body = %q", got)
	}
	want := []string{"https://u:***@h/x", "Bearer ***", "", "", "nothing here"}
	for i, kv := range rec.Attributes {
		if i == 2 {
			if got := kv.Value.GetKvlistValue().Values[0].Value.GetStringValue(); got != "token=***" {
				t.Fatalf("nested = %q", got)
			}
			continue
		}
		if i == 3 {
			continue
		}
		if got := kv.Value.GetStringValue(); got != want[i] {
			t.Fatalf("attr %s = %q, want %q", kv.Key, got, want[i])
		}
	}

	SetSignalContext(&SignalContext{LogRedactOffProbes: map[string]struct{}{"recv": {}}})
	rl = mk()
	RedactLogBatches(rl, "recv")
	if got := rl[0].ScopeLogs[0].LogRecords[0].Body.GetStringValue(); !strings.Contains(got, "passhash=123456789") {
		t.Fatalf("opted-out batch masked: %q", got)
	}
}

const typical200 = `203.0.113.7 - - [19/Oct/2026:10:00:00 +0000] "GET /api/v2/table.json?content=sensors&columns=objid,sensor&username=svc&passhash=1234567890 HTTP/1.1" 200 5120 "-" "Mozilla/5.0"`

func BenchmarkRedact200ByteLineSecret(b *testing.B) {
	_ = ConfigureLogRedaction(true, nil)
	rec := LogRecord{Body: typical200, ProducerProbeName: "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = redactLogRecord(rec)
	}
}

func BenchmarkRedact200ByteLineClean(b *testing.B) {
	_ = ConfigureLogRedaction(true, nil)
	rec := LogRecord{Body: strings.Replace(typical200, "passhash=1234567890", "limit=100", 1), ProducerProbeName: "bench"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = redactLogRecord(rec)
	}
}

func TestRedactAdversarialInputStaysLinear(t *testing.T) {
	setupRedactionTest(t)
	inputs := map[string]string{
		"spaces after key": strings.Repeat("password"+strings.Repeat(" ", 200), 2000),
		"many keys":        strings.Repeat("token=", 100000),
		"many query marks": strings.Repeat("?&key", 100000),
		"many schemes":     strings.Repeat("a://b:", 100000),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_ = redactLogRecord(LogRecord{Body: in, ProducerProbeName: "p"})
			if d := time.Since(start); d > 3*time.Second {
				t.Fatalf("took %s on %d bytes", d, len(in))
			}
		})
	}
}

func TestRedactEdgeCases(t *testing.T) {
	setupRedactionTest(t)
	cases := []struct{ name, in, want string }{
		{"empty query value", "GET /x?key=&a=1", "GET /x?key=&a=1"},
		{"separator then space", "password= next", "password= ***"},
		{"x-api-key query", "GET /x?x-api-key=abc123", "GET /x?x-api-key=***"},
		{"plural key", "tokens=5", "tokens=5"},
		{"prefixed key", "mytoken=1 _token=2", "mytoken=1 _token=2"},
		{"percent in password", "mongodb://u:p%40ss@h/db", "mongodb://u:***@h/db"},
		{"semicolon ends query value", "/p?key=a;sig=b", "/p?key=***;sig=b"},
		{"token holding a bearer", "token=Bearer abcdef0123456789ABCDEF", "token=*** ***"},
		{"already masked", "password=*** https://u:***@h/?key=***", "password=*** https://u:***@h/?key=***"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactLogRecord(LogRecord{Body: tc.in, ProducerProbeName: "p"}).Body
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestRedactIsIdempotentAndCountsOnce(t *testing.T) {
	setupRedactionTest(t)
	in := "Authorization: Basic dXNlcjpwdw== https://u:pw@h/?key=k password=p"
	once := redactLogRecord(LogRecord{Body: in, ProducerProbeName: "p"})
	counts := LogRedactedCounts()
	twice := redactLogRecord(once)
	if twice.Body != once.Body {
		t.Fatalf("second pass changed %q to %q", once.Body, twice.Body)
	}
	if !reflect.DeepEqual(counts, LogRedactedCounts()) {
		t.Fatalf("second pass moved the counters: %v then %v", counts, LogRedactedCounts())
	}
}

func TestRedactAttributeNamedAfterOAuthSecret(t *testing.T) {
	setupRedactionTest(t)
	rec := redactLogRecord(LogRecord{
		Attributes:        map[string]string{"oauth.refresh_token": "opaque", "oauth.client_secret": "opaque", "id_token": "opaque"},
		ProducerProbeName: "p",
	})
	for k, v := range rec.Attributes {
		if v != "***" {
			t.Fatalf("%s = %q", k, v)
		}
	}
}

func TestRedactLogBatchesPrivateKeyAcrossRecordsAndResource(t *testing.T) {
	setupRedactionTest(t)
	str := func(s string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
	src := []*commonpb.KeyValue{{Key: "log.file.path", Value: str("/var/log/a.log")}}
	rl := []*logspb.ResourceLogs{{
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{
			{Body: str("-----BEGIN RSA PRIVATE KEY-----"), Attributes: src},
			{Body: str("MIIEowIBAAKCAQEA0Z3VS5JJcds3xfn"), Attributes: src},
			{Body: str("-----END RSA PRIVATE KEY-----"), Attributes: src},
			{Body: str("plain line"), Attributes: src},
			{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: []byte("x")}}},
			{},
		}}},
	}}
	RedactLogBatches(rl, "recv")
	recs := rl[0].ScopeLogs[0].LogRecords
	if got := recs[1].Body.GetStringValue(); got != "***" {
		t.Fatalf("key line not masked: %q", got)
	}
	if got := recs[3].Body.GetStringValue(); got != "plain line" {
		t.Fatalf("plain line changed: %q", got)
	}
}

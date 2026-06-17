package filetail

import (
	"regexp"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func mustRegex(t *testing.T, pat string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pat)
	if err != nil {
		t.Fatalf("compile %q: %v", pat, err)
	}
	return re
}

func TestParseLine_Raw(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rec, ok := parseLine(ParserConfig{Type: ParserRaw}, "anything goes here", now, "p1", "/var/log/app.log")
	if !ok {
		t.Fatal("raw should always parse")
	}
	if rec.Body != "anything goes here" {
		t.Errorf("Body=%q", rec.Body)
	}
	if rec.Attributes["log.file.path"] != "/var/log/app.log" {
		t.Errorf("file attr=%q", rec.Attributes["log.file.path"])
	}
	if !rec.Timestamp.Equal(now) {
		t.Errorf("Timestamp=%v want %v", rec.Timestamp, now)
	}
	if rec.ProducerProbeType != ProbeType {
		t.Errorf("producer type=%q", rec.ProducerProbeType)
	}
}

func TestParseLine_Regex(t *testing.T) {
	pc := ParserConfig{
		Type:            ParserRegex,
		compiled:        mustRegex(t, `^(?P<timestamp>\S+ \S+) \[(?P<level>\w+)\] (?P<component>\w+): (?P<message>.+)$`),
		TimestampField:  "timestamp",
		TimestampFormat: "2006-01-02 15:04:05.000",
	}
	line := "2026-06-01 12:30:45.123 [ERROR] Broker: connection refused"
	rec, ok := parseLine(pc, line, time.Now(), "p1", "f")
	if !ok {
		t.Fatal("regex should parse")
	}
	if rec.Body != "connection refused" {
		t.Errorf("Body=%q", rec.Body)
	}
	if rec.Attributes["component"] != "Broker" {
		t.Errorf("component=%q", rec.Attributes["component"])
	}
	if rec.Severity != agentstate.LogSeverityError || rec.SeverityText != "ERROR" {
		t.Errorf("severity=%d text=%q", rec.Severity, rec.SeverityText)
	}
	want := time.Date(2026, 6, 1, 12, 30, 45, 123000000, time.UTC)
	if !rec.Timestamp.Equal(want) {
		t.Errorf("Timestamp=%v want %v", rec.Timestamp, want)
	}
}

func TestParseLine_RegexNoMatchKeepsRawBody(t *testing.T) {
	pc := ParserConfig{Type: ParserRegex, compiled: mustRegex(t, `^(?P<a>\d+)$`)}
	rec, ok := parseLine(pc, "not digits", time.Now(), "p1", "f")
	if !ok {
		t.Fatal("non-matching regex line should still emit raw")
	}
	if rec.Body != "not digits" {
		t.Errorf("Body=%q, want raw fallback", rec.Body)
	}
	if len(rec.Attributes) != 1 { // only log.file.path
		t.Errorf("unexpected attrs: %v", rec.Attributes)
	}
}

func TestParseLine_JSON(t *testing.T) {
	pc := ParserConfig{Type: ParserJSON, TimestampField: "ts", TimestampFormat: time.RFC3339}
	line := `{"ts":"2026-06-01T10:00:00Z","level":"warn","msg":"disk almost full","pct":95}`
	rec, ok := parseLine(pc, line, time.Now(), "p1", "f")
	if !ok {
		t.Fatal("json should parse")
	}
	if rec.Body != "disk almost full" {
		t.Errorf("Body=%q", rec.Body)
	}
	if rec.Severity != agentstate.LogSeverityWarn {
		t.Errorf("severity=%d", rec.Severity)
	}
	if rec.Attributes["pct"] != "95" {
		t.Errorf("pct=%q want 95 (integer rendered without decimal)", rec.Attributes["pct"])
	}
	want := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if !rec.Timestamp.Equal(want) {
		t.Errorf("Timestamp=%v want %v", rec.Timestamp, want)
	}
}

func TestParseLine_JSONMalformedSkips(t *testing.T) {
	pc := ParserConfig{Type: ParserJSON}
	if _, ok := parseLine(pc, "this is not json", time.Now(), "p1", "f"); ok {
		t.Errorf("non-json line under json parser should be skipped (ok=false)")
	}
}

func TestParseLine_Logfmt(t *testing.T) {
	pc := ParserConfig{Type: ParserLogfmt}
	line := `level=error msg="db connection lost" host=db01 attempts=3`
	rec, ok := parseLine(pc, line, time.Now(), "p1", "f")
	if !ok {
		t.Fatal("logfmt should parse")
	}
	if rec.Body != "db connection lost" {
		t.Errorf("Body=%q", rec.Body)
	}
	if rec.Severity != agentstate.LogSeverityError {
		t.Errorf("severity=%d", rec.Severity)
	}
	if rec.Attributes["host"] != "db01" || rec.Attributes["attempts"] != "3" {
		t.Errorf("attrs=%v", rec.Attributes)
	}
}

func TestParseTimestamp_EpochSeconds(t *testing.T) {
	got, ok := parseTimestamp("1700000000", "")
	if !ok {
		t.Fatal("epoch seconds should parse")
	}
	if !got.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("got %v", got)
	}
}

func TestParseTimestamp_EpochMillis(t *testing.T) {
	got, ok := parseTimestamp("1700000000123", "")
	if !ok {
		t.Fatal("epoch millis should parse")
	}
	if !got.Equal(time.UnixMilli(1700000000123)) {
		t.Errorf("got %v", got)
	}
}

func TestParseTimestamp_ExplicitLayoutFailure(t *testing.T) {
	if _, ok := parseTimestamp("nope", "2006-01-02"); ok {
		t.Errorf("bad value under explicit layout should fail")
	}
}

func TestParseLogfmt_QuotedAndBareKeys(t *testing.T) {
	got := parseLogfmt(`a=1 b="two words" c flag=`)
	if got["a"] != "1" {
		t.Errorf("a=%q", got["a"])
	}
	if got["b"] != "two words" {
		t.Errorf("b=%q", got["b"])
	}
	if _, ok := got["c"]; !ok {
		t.Errorf("bare key c missing")
	}
	if got["flag"] != "" {
		t.Errorf("flag=%q want empty", got["flag"])
	}
}

func TestSeverityFromText(t *testing.T) {
	cases := map[string]agentstate.LogSeverity{
		"info":     agentstate.LogSeverityInfo,
		"WARNING":  agentstate.LogSeverityWarn,
		"err":      agentstate.LogSeverityError,
		"CRITICAL": agentstate.LogSeverityFatal,
		"weird":    agentstate.LogSeverityUnspecified,
	}
	for in, want := range cases {
		if got, _ := severityFromText(in); got != want {
			t.Errorf("severityFromText(%q)=%d want %d", in, got, want)
		}
	}
}

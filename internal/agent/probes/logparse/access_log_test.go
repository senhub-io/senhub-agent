package logparse

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestSniffSeverityAccessLogShape(t *testing.T) {
	cases := []struct {
		name string
		line string
		want agentstate.LogSeverity
		text string
	}{
		{
			name: "nginx combined 200",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 200 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityInfo,
			text: "INFO",
		},
		{
			name: "nginx combined 301",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 301 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityInfo,
			text: "INFO",
		},
		{
			name: "nginx combined 404",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 404 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityWarn,
			text: "WARN",
		},
		{
			name: "nginx combined 499",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 499 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityWarn,
			text: "WARN",
		},
		{
			name: "nginx combined 502",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 502 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityError,
			text: "ERROR",
		},
		{
			name: "nginx combined with extra fields",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "POST /login HTTP/2.0" 503 0 "-" "Mozilla/5.0" "198.51.100.4" 0.004`,
			want: agentstate.LogSeverityError,
			text: "ERROR",
		},
		{
			name: "apache common 200",
			line: `203.0.113.8 - - [13/Oct/2026:10:01:02 +0200] "GET / HTTP/1.1" 200 2326`,
			want: agentstate.LogSeverityInfo,
			text: "INFO",
		},
		{
			name: "apache common 500",
			line: `198.51.100.2 - frank [13/Oct/2026:10:01:03 +0200] "POST / HTTP/1.1" 500 -`,
			want: agentstate.LogSeverityError,
			text: "ERROR",
		},
		{
			name: "ipv6 client",
			line: `2001:db8::1 - - [13/Oct/2026:10:01:04 +0200] "GET /api HTTP/1.1" 302 42`,
			want: agentstate.LogSeverityInfo,
			text: "INFO",
		},
		{
			name: "request with escaped quote",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:05 +0200] "GET /api/v1/\"x\" HTTP/1.1" 502 157 "-" "curl/8.5.0"`,
			want: agentstate.LogSeverityError,
			text: "ERROR",
		},
		{
			name: "stated level wins over 200 status",
			line: `level=error 203.0.113.7 - - [13/Oct/2026:10:01:06 +0200] "GET / HTTP/1.1" 200 10`,
			want: agentstate.LogSeverityError,
			text: "ERROR",
		},
		{
			name: "missing time brackets",
			line: `203.0.113.7 - - 13/Oct/2026:10:01:02 +0200 "GET / HTTP/1.1" 200 157`,
			want: agentstate.LogSeverityUnspecified,
			text: "",
		},
		{
			name: "4-digit status",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET / HTTP/1.1" 2000 157`,
			want: agentstate.LogSeverityUnspecified,
			text: "",
		},
		{
			name: "status 099",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET / HTTP/1.1" 099 157`,
			want: agentstate.LogSeverityUnspecified,
			text: "",
		},
		{
			name: "status 600",
			line: `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET / HTTP/1.1" 600 157`,
			want: agentstate.LogSeverityUnspecified,
			text: "",
		},
		{
			name: "plain text with quoted string and number",
			line: `plain text [not brackets] "quoted" 123 bytes`,
			want: agentstate.LogSeverityUnspecified,
			text: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, gotText := SniffSeverity(c.line)
			if got != c.want || gotText != c.text {
				t.Fatalf("SniffSeverity(%q) = %d %q, want %d %q", c.line, got, gotText, c.want, c.text)
			}
		})
	}
}

func TestParseLineRawAccessLogSetsSeverityNumber(t *testing.T) {
	line := `203.0.113.7 - - [13/Oct/2026:10:01:02 +0200] "GET /api/v1/x HTTP/1.1" 502 157 "-" "curl/8.5.0"`
	rec, ok := ParseLine(ParserConfig{Type: ParserRaw}, line, time.Now(), "p", "filetail")
	if !ok {
		t.Fatal("raw parse reported not ok")
	}
	if rec.Severity != agentstate.LogSeverityError || rec.SeverityText != "ERROR" {
		t.Fatalf("severity %d %q, want %d %q", rec.Severity, rec.SeverityText, agentstate.LogSeverityError, "ERROR")
	}
}

package syslog

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/mcuadros/go-syslog.v2/format"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// relay parses a raw datagram the way the listener does, including the
// hostname fallback to the client address, and returns the record the
// probe publishes.
func relay(t *testing.T, raw string) agentstate.LogRecord {
	t.Helper()
	parser := (&format.Automatic{}).GetParser([]byte(raw))
	if err := parser.Parse(); err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	parts := map[string]interface{}(parser.Dump())
	parts["client"] = "192.0.2.24:46784"
	if parts["hostname"] == "" {
		parts["hostname"] = "192.0.2.24"
	}

	ch := agentstate.SubscribeLogs(4)
	defer agentstate.UnsubscribeLogs(ch)
	zlog := zerolog.New(os.Stderr)
	probe := &SyslogProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       SyslogProbeConfig{Port: DefaultPort, Protocol: DefaultProtocol},
		moduleLogger: logger.NewModuleLogger((*logger.Logger)(&zlog), "probe.syslog.test"),
	}
	probe.processLogMessage(parts)
	select {
	case rec := <-ch:
		return rec
	case <-time.After(time.Second):
		t.Fatal("no log record published")
	}
	return agentstate.LogRecord{}
}

func TestProcessLogMessage_CEFWithoutPRI(t *testing.T) {
	cef := "CEF:0|Ubiquiti|UniFi Network|10.6.106|546|Config Modified|5|UNIFIcategory=Audit msg=admin made a change"
	rec := relay(t, "Sep 29 18:08:37 unifi-pi "+cef)

	if rec.Body != cef {
		t.Errorf("body = %q, want the CEF record without the header", rec.Body)
	}
	if got := rec.Attributes["syslog.hostname"]; got != "unifi-pi" {
		t.Errorf("syslog.hostname = %q, want unifi-pi", got)
	}
	if got := rec.Attributes["syslog.appname"]; got != "" {
		t.Errorf("syslog.appname = %q, want empty", got)
	}
	if got := rec.Attributes["syslog.severity_code"]; got != "4" {
		t.Errorf("syslog.severity_code = %q, want 4 (CEF severity 5)", got)
	}
	if rec.Severity != agentstate.SyslogPriorityToSeverity(4) {
		t.Errorf("severity = %v, want warning", rec.Severity)
	}
	for _, k := range []string{"syslog.facility", "syslog.priority"} {
		if v, ok := rec.Attributes[k]; ok {
			t.Errorf("%s = %q reported although the sender sent no PRI", k, v)
		}
	}
	if rec.Timestamp.Month() != time.September || rec.Timestamp.Day() != 29 || rec.Timestamp.Hour() != 18 {
		t.Errorf("timestamp = %v, want the header's", rec.Timestamp)
	}
}

func TestProcessLogMessage_TaggedLineWithoutPRI(t *testing.T) {
	rec := relay(t, "Sep 29 18:08:37 node-1 sshd[812]: Accepted publickey")
	if rec.Body != "Accepted publickey" {
		t.Errorf("body = %q", rec.Body)
	}
	if got := rec.Attributes["syslog.hostname"]; got != "node-1" {
		t.Errorf("syslog.hostname = %q, want node-1", got)
	}
	if got := rec.Attributes["syslog.appname"]; got != "sshd" {
		t.Errorf("syslog.appname = %q, want sshd", got)
	}
	if got := rec.Attributes["syslog.severity_code"]; got != "5" {
		t.Errorf("syslog.severity_code = %q, want 5", got)
	}
}

func TestProcessLogMessage_CEFWithPRIKeepsTheSenderSeverity(t *testing.T) {
	rec := relay(t, "<11>Sep 29 18:08:37 fw CEF:0|V|P|1|100|Blocked|9|src=10.0.0.1")
	if got := rec.Attributes["syslog.severity_code"]; got != "3" {
		t.Errorf("syslog.severity_code = %q, want 3 from the PRI", got)
	}
	if got := rec.Attributes["syslog.priority"]; got != "11" {
		t.Errorf("syslog.priority = %q, want 11", got)
	}
}

func TestParseBareHeader_YearRollover(t *testing.T) {
	now := time.Date(2027, time.January, 1, 0, 5, 0, 0, time.UTC)
	hdr, ok := parseBareHeader("Dec 31 23:59:58 host message", now)
	if !ok {
		t.Fatal("header not recognised")
	}
	if hdr.timestamp.Year() != 2026 {
		t.Errorf("year = %d, want 2026", hdr.timestamp.Year())
	}
}

func TestParseBareHeader_RejectsPlainText(t *testing.T) {
	for _, s := range []string{
		"connection reset by peer",
		"Sep 29 18:08:37 sshd: no hostname",
		"Sep 29 18:08:37 host",
	} {
		if _, ok := parseBareHeader(s, time.Now()); ok {
			t.Errorf("%q taken for a header", s)
		}
	}
}

func TestCEFSeverity(t *testing.T) {
	cases := map[string]int{"0": 6, "3": 6, "4": 4, "6": 4, "7": 3, "8": 3, "9": 2, "10": 2, "Low": 6, "Medium": 4, "High": 3, "Very-High": 2}
	for level, want := range cases {
		got, ok := cefSeverity("CEF:0|V|P|1|1|N|" + level + "|x=y")
		if !ok || got != want {
			t.Errorf("CEF severity %s = %d (%v), want %d", level, got, ok, want)
		}
	}
	for _, s := range []string{"CEF:0|V|P|1|1|N|11|x", "CEF:0|V|P|1|1|N|urgent|x", "plain text"} {
		if _, ok := cefSeverity(s); ok {
			t.Errorf("%q gave a severity", s)
		}
	}
}

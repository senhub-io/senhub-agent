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

type panickingParser struct{ format.LogParser }

func (panickingParser) Parse() error            { panic("index out of range") }
func (panickingParser) Dump() format.LogParts   { panic("index out of range") }
func (panickingParser) Location(*time.Location) {}

func TestSafeParserContainsAParserPanic(t *testing.T) {
	parser := &safeParser{LogParser: panickingParser{}, line: "raw line"}
	if err := parser.Parse(); err == nil {
		t.Fatal("expected the parser panic to surface as an error")
	}
	if got := parser.Dump()["content"]; got != "raw line" {
		t.Errorf("content = %v, want the raw line", got)
	}
}

func TestUnparsedMessageHasNoSeverity(t *testing.T) {
	parser := &safeParser{LogParser: panickingParser{}, line: "raw line"}
	_ = parser.Parse()
	parts := map[string]interface{}(parser.Dump())
	parts["client"] = "192.0.2.24:46784"

	ch := agentstate.SubscribeLogs(4)
	defer agentstate.UnsubscribeLogs(ch)
	zlog := zerolog.New(os.Stderr)
	probe := &SyslogProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       SyslogProbeConfig{Port: DefaultPort, Protocol: DefaultProtocol},
		moduleLogger: logger.NewModuleLogger((*logger.Logger)(&zlog), "probe.syslog.test"),
	}
	probe.processLogMessage(parts)

	var rec agentstate.LogRecord
	select {
	case rec = <-ch:
	case <-time.After(time.Second):
		t.Fatal("no log record published")
	}
	if rec.Severity != agentstate.LogSeverityUnspecified || rec.SeverityText != "" {
		t.Errorf("severity = %v %q, want unspecified: an unparsed message must not read as emergency", rec.Severity, rec.SeverityText)
	}
	for _, k := range []string{"syslog.severity_code", "syslog.facility", "syslog.priority"} {
		if v, ok := rec.Attributes[k]; ok {
			t.Errorf("%s = %q on a message whose header was not read", k, v)
		}
	}
	if rec.Body != "raw line" {
		t.Errorf("body = %q, want the raw line", rec.Body)
	}
}

func TestEmptyFrameIsNotARecord(t *testing.T) {
	parser := (&safeFormat{}).GetParser([]byte(""))
	_ = parser.Parse()
	parts := map[string]interface{}(parser.Dump())
	parts["client"] = "192.0.2.24:46784"

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
		t.Fatalf("an empty frame published a record: %+v", rec)
	case <-time.After(100 * time.Millisecond):
	}
}

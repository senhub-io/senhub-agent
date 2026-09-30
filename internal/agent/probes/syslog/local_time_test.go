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

// An RFC 3164 header carries the sender's local time with no zone. The
// parser go-syslog embeds reads it as UTC, so on a host in Paris a UniFi
// access point's "Sep 30 09:49:59" landed in the log store at 09:49:59Z,
// two hours in the future, and a search on the last minutes found nothing.
func TestAnRFC3164TimestampIsReadInTheHostZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tz database")
	}
	defer func(l *time.Location) { time.Local = l }(time.Local)
	time.Local = paris

	parser := (&format.RFC3164{}).GetParser([]byte("<15>Sep 30 09:49:59 Openspace hostapd[10060]: station associated"))
	if err := parser.Parse(); err != nil {
		t.Fatal(err)
	}
	parts := map[string]interface{}(parser.Dump())

	ch := agentstate.SubscribeLogs(4)
	defer agentstate.UnsubscribeLogs(ch)
	zlog := zerolog.New(os.Stderr)
	base := logger.NewModuleLogger((*logger.Logger)(&zlog), "probe.syslog.test")
	probe := &SyslogProbe{BaseProbe: &types.BaseProbe{}, config: SyslogProbeConfig{Port: DefaultPort, Protocol: DefaultProtocol}, moduleLogger: base}
	probe.processLogMessage(parts)

	select {
	case rec := <-ch:
		want := time.Date(time.Now().Year(), time.September, 30, 7, 49, 59, 0, time.UTC)
		if !rec.Timestamp.Equal(want) {
			t.Fatalf("timestamp = %s, want %s (09:49:59 in Paris)", rec.Timestamp.UTC(), want)
		}
	case <-time.After(time.Second):
		t.Fatal("no log record published")
	}
}

// An RFC 5424 timestamp carries its offset and is kept as sent.
func TestAnRFC5424TimestampKeepsItsOffset(t *testing.T) {
	ts := time.Date(2026, 9, 30, 9, 49, 59, 0, time.FixedZone("", 2*3600))
	if got := hostLocalWallClock(ts); !got.Equal(ts) {
		t.Fatalf("got %s, want %s", got, ts)
	}
}

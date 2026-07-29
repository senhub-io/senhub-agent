package event

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

// newPumpTestStrategy builds a strategy and starts ONLY its log-bus pump
// (not the ticker), so the test owns the buffer without doSync competing.
func newPumpTestStrategy(t *testing.T) *EventSyncStrategy {
	t.Helper()
	s, err := NewEventSyncStrategy(stubAgentConfig{},
		configuration.StorageConfigParams{"server_url": "http://127.0.0.1:9"}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.startLogPump()
	t.Cleanup(func() {
		if s.logCancel != nil {
			s.logCancel()
			s.logWG.Wait()
			agentstate.UnsubscribeLogs(s.logSub)
		}
	})
	return s
}

// TestLogPump_SyslogReachesEventInsert verifies a syslog log record is
// converted and enqueued for /event/insert (#294 step 1a).
func TestLogPump_SyslogReachesEventInsert(t *testing.T) {
	s := newPumpTestStrategy(t)

	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp: time.Unix(1_700_000_000, 0),
		Body:      "hello from syslog",
		Attributes: map[string]string{
			"syslog.severity_code": "6",
			"syslog.hostname":      "web01",
			"syslog.facility":      "1",
			"syslog.priority":      "14",
			"syslog.appname":       "sshd",
			"syslog.client":        "10.0.0.1",
		},
		ProducerProbeType: "syslog",
	})

	select {
	case evt := <-s.buffer:
		if evt["message"] != "hello from syslog" {
			t.Errorf("message = %v", evt["message"])
		}
		if evt["host"] != "web01" {
			t.Errorf("host = %v", evt["host"])
		}
		if evt["severity"] == nil {
			t.Errorf("severity missing: %v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("syslog log did not reach the event buffer")
	}
}

// TestLogPump_NonSyslogDoesNotLeak is the anti-leak guard: only syslog
// records feed /event/insert. A filetail (or any other) log must never be
// posted to the legacy rail (#294 step 1a).
func TestLogPump_NonSyslogDoesNotLeak(t *testing.T) {
	s := newPumpTestStrategy(t)

	// A non-syslog producer, then a syslog one published after it. If the
	// filter were broken, the filetail event would arrive first.
	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp:         time.Unix(1_700_000_001, 0),
		Body:              "a tailed file line",
		ProducerProbeType: "filetail",
	})
	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp:         time.Unix(1_700_000_002, 0),
		Body:              "syslog marker",
		Attributes:        map[string]string{"syslog.severity_code": "6", "syslog.hostname": "h"},
		ProducerProbeType: "syslog",
	})

	select {
	case evt := <-s.buffer:
		if evt["message"] != "syslog marker" {
			t.Fatalf("non-syslog log leaked onto /event/insert: %v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("syslog marker did not arrive")
	}

	// Nothing else should be queued (the filetail log was dropped).
	select {
	case evt := <-s.buffer:
		t.Errorf("unexpected second event on /event/insert: %v", evt)
	case <-time.After(200 * time.Millisecond):
	}
}

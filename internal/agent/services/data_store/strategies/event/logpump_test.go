package event

import (
	"context"
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
	s.startLogPump(context.Background())
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

// tagsAgentConfig is a stub whose global_tags are non-empty, to exercise the
// M2 re-injection.
type tagsAgentConfig struct{ stubAgentConfig }

func (tagsAgentConfig) GetConfigPath() string { return "" }
func (tagsAgentConfig) GetGlobalTags() map[string]string {
	return map[string]string{"site": "paris", "env": "prod"}
}

// TestLogPump_ReinjectsGlobalTags is the M2 regression guard: the old
// datapoint path ran through the DataStore's enrichWithConfiguredTags, so
// agent global_tags appeared as fields in every /event/insert event. The log
// bus bypasses the DataStore, so the pump must re-apply them or the payload
// silently loses site/env for any deployment with global_tags configured.
func TestLogPump_ReinjectsGlobalTags(t *testing.T) {
	s, err := NewEventSyncStrategy(tagsAgentConfig{},
		configuration.StorageConfigParams{"server_url": "http://127.0.0.1:9"}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewEventSyncStrategy: %v", err)
	}
	s.startLogPump(context.Background())
	t.Cleanup(func() {
		if s.logCancel != nil {
			s.logCancel()
			s.logWG.Wait()
			agentstate.UnsubscribeLogs(s.logSub)
		}
	})

	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp:         time.Unix(1_700_000_000, 0),
		Body:              "hi",
		Attributes:        map[string]string{"syslog.severity_code": "6", "syslog.hostname": "h"},
		ProducerProbeType: "syslog",
	})

	select {
	case evt := <-s.buffer:
		if evt["site"] != "paris" || evt["env"] != "prod" {
			t.Errorf("global_tags not re-injected onto /event/insert: %v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("syslog log did not reach the event buffer")
	}
}

// TestLogPump_EventReachesEventInsert verifies an event-probe LogRecord —
// whose structured payload rides Fields — is converted via FromEventLog and
// enqueued for /event/insert (#294 step 1b), with structure preserved.
func TestLogPump_EventReachesEventInsert(t *testing.T) {
	s := newPumpTestStrategy(t)

	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp: time.Unix(1_700_000_000, 0),
		Fields: map[string]any{
			"host":     "app-1",
			"message":  "job done",
			"severity": "Info",
			"targets":  []any{"a", "b"},
		},
		ProducerProbeType: "event",
	})

	select {
	case evt := <-s.buffer:
		if evt["message"] != "job done" || evt["host"] != "app-1" {
			t.Errorf("required fields wrong: %v", evt)
		}
		if _, isSlice := evt["targets"].([]any); !isSlice {
			t.Errorf("structured field lost: targets=%T (%v)", evt["targets"], evt["targets"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event log did not reach the event buffer")
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

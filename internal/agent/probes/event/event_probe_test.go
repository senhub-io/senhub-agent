package event

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func newEventProbeTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newEventProbeForTest(t *testing.T, config map[string]interface{}) *EventProbe {
	t.Helper()
	probe, err := NewEventProbe(config, newEventProbeTestLogger())
	if err != nil {
		t.Fatalf("NewEventProbe: %v", err)
	}
	p, ok := probe.(*EventProbe)
	if !ok {
		t.Fatalf("unexpected probe type %T", probe)
	}
	return p
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		<-ticker.C
	}
	t.Fatalf("condition was not met within %v", timeout)
}

func waitForListenerHealthError(t *testing.T, p *EventProbe, timeout time.Duration, fragment string) error {
	t.Helper()
	var found error
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		err := p.ListenerHealth()
		if err != nil && strings.Contains(err.Error(), fragment) {
			found = err
			break
		}
		<-ticker.C
	}
	if found == nil {
		t.Fatalf("listener health did not report %q within %v", fragment, timeout)
	}
	return found
}

func acceptedSeverityByOTel(t *testing.T, want agentstate.LogSeverity) string {
	t.Helper()
	for _, name := range agentstate.EventProbeSeverityNames() {
		if sev, ok := agentstate.EventProbeSeverityToOTel(name); ok && sev == want {
			return name
		}
	}
	t.Fatalf("no accepted severity maps to %d", want)
	return ""
}

func postEventRequest(t *testing.T, event map[string]interface{}) *http.Request {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/event", strings.NewReader(string(body)))
}

func receiveLogBody(t *testing.T, ch <-chan agentstate.LogRecord, body string, timeout time.Duration) agentstate.LogRecord {
	t.Helper()
	timeoutCh := time.After(timeout)
	for {
		select {
		case rec := <-ch:
			if rec.Body == body {
				return rec
			}
		case <-timeoutCh:
			t.Fatalf("timed out waiting for log record with body %q", body)
		}
	}
}

func TestNewEventProbeDefaults(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{})
	if p.config.Address != DefaultAddress {
		t.Errorf("Address = %q, want %q", p.config.Address, DefaultAddress)
	}
	if p.config.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", p.config.Port, DefaultPort)
	}
	if p.config.Protocol != DefaultProtocol {
		t.Errorf("Protocol = %q, want %q", p.config.Protocol, DefaultProtocol)
	}
}

func TestNewEventProbeRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, 65536} {
		t.Run(fmt.Sprintf("port %d", port), func(t *testing.T) {
			_, err := NewEventProbe(map[string]interface{}{"port": port}, newEventProbeTestLogger())
			if err == nil {
				t.Fatalf("port %d must be rejected", port)
			}
			if !strings.Contains(err.Error(), "port must be between 1 and 65535") {
				t.Errorf("error %q does not explain the valid port range", err)
			}
		})
	}
}

func TestEventProbeStrategiesIntervalAndCollect(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{})

	targets := p.GetTargetStrategies()
	if len(targets) != 1 || targets[0] != "event" {
		t.Errorf("GetTargetStrategies() = %v, want [event]", targets)
	}
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
	if p.GetInterval() != DefaultSyncInterval {
		t.Errorf("GetInterval() = %v, want %v", p.GetInterval(), DefaultSyncInterval)
	}
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if points != nil {
		t.Errorf("Collect() points = %v, want nil", points)
	}
}

func TestListenerHealthBeforeStartReportsNotRunning(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{})
	err := p.ListenerHealth()
	if err == nil {
		t.Fatal("ListenerHealth() = nil, want an error before OnStart")
	}
	if !strings.Contains(err.Error(), "HTTP listener is not running") {
		t.Errorf("ListenerHealth() error = %v, want 'HTTP listener is not running'", err)
	}
}

func TestListenerHealthBecomesServingAfterStart(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{
		"address": "127.0.0.1",
		"port":    freeTCPPort(t),
	})
	quit := make(chan struct{})
	if err := p.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.OnShutdown(ctx)
	})

	waitForCondition(t, 2*time.Second, func() bool {
		return p.ListenerHealth() == nil
	})
}

func TestListenerHealthReportsBindFailure(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	p := newEventProbeForTest(t, map[string]interface{}{
		"address": "127.0.0.1",
		"port":    port,
	})
	quit := make(chan struct{})
	if err := p.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.OnShutdown(ctx)
	})

	err = waitForListenerHealthError(t, p, 2*time.Second, "bind")
	if !strings.Contains(err.Error(), "bind") {
		t.Errorf("ListenerHealth() error = %v, want it to carry the bind failure", err)
	}
}

func TestListenerHealthAfterShutdownReportsNotRunning(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{
		"address": "127.0.0.1",
		"port":    freeTCPPort(t),
	})
	quit := make(chan struct{})
	if err := p.OnStart(quit); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return p.ListenerHealth() == nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.OnShutdown(ctx); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}

	err := p.ListenerHealth()
	if err == nil {
		t.Fatal("ListenerHealth() = nil, want an error after OnShutdown")
	}
	if !strings.Contains(err.Error(), "HTTP listener is not running") {
		t.Errorf("ListenerHealth() error = %v, want 'HTTP listener is not running'", err)
	}
}

func TestHandleEventRejectsNonPost(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{})
	req := httptest.NewRequest(http.MethodGet, "/event", nil)
	rec := httptest.NewRecorder()

	p.handleEvent(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Invalid request method") {
		t.Errorf("body = %q, want 'Invalid request method'", body)
	}
}

func TestHandleEventRejectsMalformedJSON(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/event", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()

	p.handleEvent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Invalid request body") {
		t.Errorf("body = %q, want 'Invalid request body'", body)
	}
}

func TestHandleEventValidationFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]interface{})
		want   string
	}{
		{
			name: "missing host",
			mutate: func(m map[string]interface{}) {
				delete(m, "host")
			},
			want: "missing required field: host",
		},
		{
			name: "missing message",
			mutate: func(m map[string]interface{}) {
				delete(m, "message")
			},
			want: "missing required field: message",
		},
		{
			name: "missing severity",
			mutate: func(m map[string]interface{}) {
				delete(m, "severity")
			},
			want: "missing required field: severity",
		},
		{
			name: "too many fields",
			mutate: func(m map[string]interface{}) {
				for i := 0; i < 17; i++ {
					m[fmt.Sprintf("extra%d", i)] = i
				}
			},
			want: "too many fields, maximum allowed is 20",
		},
		{
			name: "bad timestamp",
			mutate: func(m map[string]interface{}) {
				m["timestamp"] = "yesterday"
			},
			want: "invalid timestamp format, must be ISO8601",
		},
		{
			name: "empty host",
			mutate: func(m map[string]interface{}) {
				m["host"] = ""
			},
			want: "host must be a non-empty string",
		},
		{
			name: "empty message",
			mutate: func(m map[string]interface{}) {
				m["message"] = ""
			},
			want: "message must be a non-empty string",
		},
		{
			name: "non-string severity",
			mutate: func(m map[string]interface{}) {
				m["severity"] = 42
			},
			want: "severity must be a string",
		},
		{
			name: "invalid severity",
			mutate: func(m map[string]interface{}) {
				m["severity"] = "bogus"
			},
			want: "invalid severity value: bogus",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := map[string]interface{}{
				"host":      "host-1",
				"message":   "hello",
				"severity":  acceptedSeverityByOTel(t, agentstate.LogSeverityInfo),
				"timestamp": "2024-05-06T07:08:09Z",
			}
			tc.mutate(event)

			p := newEventProbeForTest(t, map[string]interface{}{})
			req := postEventRequest(t, event)
			rec := httptest.NewRecorder()

			p.handleEvent(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.want) {
				t.Errorf("body = %q, want it to contain %q", body, tc.want)
			}
		})
	}
}

func TestValidateEventAcceptsAllSeverityNames(t *testing.T) {
	for _, severity := range agentstate.EventProbeSeverityNames() {
		t.Run(severity, func(t *testing.T) {
			event := map[string]interface{}{
				"host":     "host-1",
				"message":  "hello",
				"severity": severity,
			}
			if err := validateEvent(event); err != nil {
				t.Errorf("validateEvent(%q) = %v, want nil", severity, err)
			}
		})
	}
}

func TestHandleEventPublishesLogRecord(t *testing.T) {
	severity := acceptedSeverityByOTel(t, agentstate.LogSeverityInfo)
	ch := agentstate.SubscribeLogsFor("event", 16)
	defer agentstate.UnsubscribeLogs(ch)

	p := newEventProbeForTest(t, map[string]interface{}{})
	uniqueMessage := fmt.Sprintf("event-probe-test-%d", time.Now().UnixNano())
	const timestamp = "2024-05-06T07:08:09Z"
	event := map[string]interface{}{
		"host":      "host-a",
		"message":   uniqueMessage,
		"severity":  severity,
		"timestamp": timestamp,
		"extra":     42,
	}
	if err := validateEvent(event); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}

	req := postEventRequest(t, event)
	rec := httptest.NewRecorder()
	p.handleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Event processed successfully") {
		t.Errorf("body = %q, want 'Event processed successfully'", body)
	}

	got := receiveLogBody(t, ch, uniqueMessage, 2*time.Second)

	wantTime, _ := time.Parse(time.RFC3339, timestamp)
	if !got.Timestamp.Equal(wantTime) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, wantTime)
	}
	if got.Severity != agentstate.LogSeverityInfo {
		t.Errorf("Severity = %v, want %v", got.Severity, agentstate.LogSeverityInfo)
	}
	if got.SeverityText != severity {
		t.Errorf("SeverityText = %q, want %q", got.SeverityText, severity)
	}
	if got.Body != uniqueMessage {
		t.Errorf("Body = %q, want %q", got.Body, uniqueMessage)
	}
	if got.ProducerProbeType != "event" {
		t.Errorf("ProducerProbeType = %q, want 'event'", got.ProducerProbeType)
	}
	if got.Attributes["event.host"] != "host-a" {
		t.Errorf("Attributes[event.host] = %q, want %q", got.Attributes["event.host"], "host-a")
	}
	if got.Attributes["event.severity"] != severity {
		t.Errorf("Attributes[event.severity] = %q, want %q", got.Attributes["event.severity"], severity)
	}
	if got.Attributes["senhub.event.extra"] != "42" {
		t.Errorf("Attributes[senhub.event.extra] = %q, want %q", got.Attributes["senhub.event.extra"], "42")
	}

	select {
	case extra := <-ch:
		if extra.Body == uniqueMessage {
			t.Fatal("duplicate log record published for the same event")
		}
	default:
	}
}

func TestParseEventTimestampHonoursRFC3339(t *testing.T) {
	event := map[string]interface{}{"timestamp": "2024-05-06T07:08:09Z"}
	got := parseEventTimestamp(event)
	want := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseEventTimestamp() = %v, want %v", got, want)
	}
}

func TestParseEventTimestampFallsBackToNow(t *testing.T) {
	for _, name := range []string{"invalid", "absent"} {
		t.Run(name, func(t *testing.T) {
			before := time.Now()
			var event map[string]interface{}
			if name == "invalid" {
				event = map[string]interface{}{"timestamp": "yesterday"}
			} else {
				event = map[string]interface{}{}
			}
			got := parseEventTimestamp(event)
			after := time.Now()
			if got.Before(before) || got.After(after) {
				t.Errorf("parseEventTimestamp(%v) = %v, want between %v and %v", event, got, before, after)
			}
		})
	}
}

func TestEventProbeSeverity(t *testing.T) {
	if got := eventProbeSeverity("does-not-exist-in-accepted-list"); got != agentstate.LogSeverityUnspecified {
		t.Errorf("eventProbeSeverity(invalid) = %v, want %v", got, agentstate.LogSeverityUnspecified)
	}

	name := acceptedSeverityByOTel(t, agentstate.LogSeverityError)
	if got := eventProbeSeverity(name); got != agentstate.LogSeverityError {
		t.Errorf("eventProbeSeverity(%q) = %v, want %v", name, got, agentstate.LogSeverityError)
	}
}

func TestEventProbeString(t *testing.T) {
	p := newEventProbeForTest(t, map[string]interface{}{
		"address":  "10.0.0.1",
		"port":     1234,
		"protocol": "tcp",
	})
	want := "EventProbe{address=10.0.0.1, port=1234, protocol=tcp}"
	if got := p.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

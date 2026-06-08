package windowseventlog

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const sampleSecurityEvent = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'>
  <System>
    <Provider Name='Microsoft-Windows-Security-Auditing' Guid='{54849625-5478-4994-A5BA-3E3B0328C30D}'/>
    <EventID>4624</EventID>
    <Level>0</Level>
    <Task>12544</Task>
    <TimeCreated SystemTime='2026-06-01T12:00:00.1234567Z'/>
    <EventRecordID>987654</EventRecordID>
    <Execution ProcessID='678' ThreadID='789'/>
    <Channel>Security</Channel>
    <Computer>WIN-VDA01</Computer>
    <Security UserID='S-1-5-18'/>
  </System>
  <EventData>
    <Data Name='TargetUserName'>jdoe</Data>
    <Data Name='IpAddress'>10.0.0.5</Data>
    <Data Name='LogonType'>3</Data>
  </EventData>
  <RenderingInfo Culture='en-US'>
    <Message>An account was successfully logged on.</Message>
    <Level>Information</Level>
  </RenderingInfo>
</Event>`

func TestParseEventXML_PopulatesFields(t *testing.T) {
	ev, ok := parseEventXML(sampleSecurityEvent)
	if !ok {
		t.Fatal("parseEventXML returned ok=false for a valid document")
	}
	if ev.EventID != 4624 {
		t.Errorf("EventID = %d, want 4624", ev.EventID)
	}
	if ev.Provider != "Microsoft-Windows-Security-Auditing" {
		t.Errorf("Provider = %q", ev.Provider)
	}
	if ev.Channel != "Security" {
		t.Errorf("Channel = %q", ev.Channel)
	}
	if ev.Computer != "WIN-VDA01" {
		t.Errorf("Computer = %q", ev.Computer)
	}
	if ev.RecordID != "987654" {
		t.Errorf("RecordID = %q", ev.RecordID)
	}
	if ev.Message != "An account was successfully logged on." {
		t.Errorf("Message = %q", ev.Message)
	}
	if ev.EventData["TargetUserName"] != "jdoe" || ev.EventData["IpAddress"] != "10.0.0.5" {
		t.Errorf("EventData = %v", ev.EventData)
	}
	want := time.Date(2026, 6, 1, 12, 0, 0, 123456700, time.UTC)
	if !ev.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, want)
	}
}

func TestParseEventXML_FallbackMessage(t *testing.T) {
	const noRendering = `<Event><System>
		<Provider Name='Custom-Provider'/>
		<EventID>1024</EventID><Level>2</Level>
		<Channel>Application</Channel></System>
		<EventData><Data Name='Path'>C:\app.exe</Data></EventData></Event>`
	ev, ok := parseEventXML(noRendering)
	if !ok {
		t.Fatal("ok=false")
	}
	if !strings.Contains(ev.Message, "Custom-Provider event 1024") {
		t.Errorf("fallback message = %q", ev.Message)
	}
	if !strings.Contains(ev.Message, "Path=C:\\app.exe") {
		t.Errorf("fallback should include EventData: %q", ev.Message)
	}
}

func TestParseEventXML_RejectsGarbage(t *testing.T) {
	if _, ok := parseEventXML("not xml at all <<<"); ok {
		t.Error("expected ok=false for malformed XML")
	}
}

func TestParseSystemTime_FallsBackToNow(t *testing.T) {
	before := time.Now()
	got := parseSystemTime("")
	if got.Before(before.Add(-time.Second)) {
		t.Errorf("empty SystemTime should fall back to ~now, got %v", got)
	}
}

func TestWinLevelMapping(t *testing.T) {
	cases := []struct {
		level int
		text  string
		sev   agentstate.LogSeverity
	}{
		{1, "Critical", agentstate.LogSeverityFatal},
		{2, "Error", agentstate.LogSeverityError},
		{3, "Warning", agentstate.LogSeverityWarn},
		{4, "Information", agentstate.LogSeverityInfo},
		{5, "Verbose", agentstate.LogSeverityDebug},
		{0, "Information", agentstate.LogSeverityInfo}, // LogAlways
	}
	for _, c := range cases {
		if got := winLevelToText(c.level); got != c.text {
			t.Errorf("winLevelToText(%d) = %q want %q", c.level, got, c.text)
		}
		if got := winLevelToSeverity(c.level); got != c.sev {
			t.Errorf("winLevelToSeverity(%d) = %d want %d", c.level, got, c.sev)
		}
	}
}

func TestToLogRecord_MandatedAttributes(t *testing.T) {
	ev, _ := parseEventXML(sampleSecurityEvent)
	rec := ev.toLogRecord("vda_citrix_events", false)

	if rec.ProducerProbeType != ProbeType {
		t.Errorf("ProducerProbeType = %q want %q", rec.ProducerProbeType, ProbeType)
	}
	if rec.ProducerProbeName != "vda_citrix_events" {
		t.Errorf("ProducerProbeName = %q", rec.ProducerProbeName)
	}
	// Issue #154 mandated attribute keys.
	for _, k := range []string{"event_id", "event_level", "event_channel", "event_provider", "event_source", "record_id"} {
		if _, ok := rec.Attributes[k]; !ok {
			t.Errorf("missing mandated attribute %q", k)
		}
	}
	if rec.Attributes["event_id"] != "4624" {
		t.Errorf("event_id = %q", rec.Attributes["event_id"])
	}
	if rec.Attributes["host.name"] != "WIN-VDA01" {
		t.Errorf("host.name = %q", rec.Attributes["host.name"])
	}
	if rec.Attributes["eventdata.TargetUserName"] != "jdoe" {
		t.Errorf("eventdata.TargetUserName = %q", rec.Attributes["eventdata.TargetUserName"])
	}
}

func TestToLogRecord_RedactPII(t *testing.T) {
	ev, _ := parseEventXML(sampleSecurityEvent)
	rec := ev.toLogRecord("sec", true)

	if got := rec.Attributes["eventdata.TargetUserName"]; got != redactedPlaceholder {
		t.Errorf("TargetUserName should be redacted, got %q", got)
	}
	if got := rec.Attributes["eventdata.IpAddress"]; got != redactedPlaceholder {
		t.Errorf("IpAddress should be redacted, got %q", got)
	}
	if got := rec.Attributes["eventdata.LogonType"]; got != "3" {
		t.Errorf("non-sensitive LogonType should survive, got %q", got)
	}
	if !strings.Contains(rec.Body, "REDACTED") {
		t.Errorf("Security body should be redacted, got %q", rec.Body)
	}
}

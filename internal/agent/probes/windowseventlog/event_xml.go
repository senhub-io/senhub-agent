package windowseventlog

import (
	"encoding/xml"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// renderedEvent models the subset of the Windows Event Log XML schema we
// translate into an OTel log record. The wevtapi EvtRender call returns
// each event as an XML document conforming to the Event schema:
// https://learn.microsoft.com/windows/win32/wes/eventschema-schema
//
// We unmarshal the fields useful for diagnostics and carry the
// remaining EventData entries into the structured attributes map so
// downstream operators keep access to the full payload.
type renderedEvent struct {
	XMLName xml.Name `xml:"Event"`
	System  struct {
		Provider struct {
			Name string `xml:"Name,attr"`
			GUID string `xml:"Guid,attr"`
		} `xml:"Provider"`
		EventID     string `xml:"EventID"`
		Level       string `xml:"Level"`
		Task        string `xml:"Task"`
		Keywords    string `xml:"Keywords"`
		Channel     string `xml:"Channel"`
		Computer    string `xml:"Computer"`
		EventRecord string `xml:"EventRecordID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		Execution struct {
			ProcessID string `xml:"ProcessID,attr"`
			ThreadID  string `xml:"ThreadID,attr"`
		} `xml:"Execution"`
		Security struct {
			UserID string `xml:"UserID,attr"`
		} `xml:"Security"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
	RenderingInfo struct {
		Message string `xml:"Message"`
		Level   string `xml:"Level"`
	} `xml:"RenderingInfo"`
}

// parsedEvent is the OS-agnostic intermediate the probe filters on. It
// is produced from the raw XML and consumed both by the filter logic
// (testable on any platform) and by the LogRecord builder.
type parsedEvent struct {
	EventID   int
	Level     int
	Provider  string
	Channel   string
	Computer  string
	RecordID  string
	ProcessID string
	UserID    string
	Message   string
	Timestamp time.Time
	EventData map[string]string
}

// parseEventXML decodes one rendered Event Log XML document into a
// parsedEvent. Returns ok=false when the document cannot be decoded —
// the caller logs and skips rather than aborting the stream, mirroring
// the linux_logs single-bad-line tolerance.
func parseEventXML(raw string) (parsedEvent, bool) {
	var ev renderedEvent
	if err := xml.Unmarshal([]byte(raw), &ev); err != nil {
		return parsedEvent{}, false
	}

	eventID, _ := strconv.Atoi(strings.TrimSpace(ev.System.EventID))
	level, _ := strconv.Atoi(strings.TrimSpace(ev.System.Level))

	data := make(map[string]string, len(ev.EventData.Data))
	for i, d := range ev.EventData.Data {
		key := d.Name
		if key == "" {
			// Unnamed <Data> entries are positional in the schema.
			key = "Data" + strconv.Itoa(i)
		}
		data[key] = strings.TrimSpace(d.Value)
	}

	message := strings.TrimSpace(ev.RenderingInfo.Message)
	if message == "" {
		message = renderFallbackMessage(eventID, ev.System.Provider.Name, data)
	}

	return parsedEvent{
		EventID:   eventID,
		Level:     level,
		Provider:  ev.System.Provider.Name,
		Channel:   ev.System.Channel,
		Computer:  ev.System.Computer,
		RecordID:  strings.TrimSpace(ev.System.EventRecord),
		ProcessID: ev.System.Execution.ProcessID,
		UserID:    ev.System.Security.UserID,
		Message:   message,
		Timestamp: parseSystemTime(ev.System.TimeCreated.SystemTime),
		EventData: data,
	}, true
}

// renderFallbackMessage builds a human-readable body when the rendered
// message is absent — common when the provider's message DLL is not
// installed on the collecting host. We do not have the publisher's
// message template, so we surface the raw EventData as a compact
// key=value list keyed by EventID, which is enough for triage. Keys are
// emitted in sorted order for deterministic output.
func renderFallbackMessage(eventID int, provider string, data map[string]string) string {
	var b strings.Builder
	b.WriteString(provider)
	b.WriteString(" event ")
	b.WriteString(strconv.Itoa(eventID))
	if len(data) > 0 {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sortStrings(keys)
		b.WriteString(": ")
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(data[k])
		}
	}
	return b.String()
}

// parseSystemTime converts the Event schema SystemTime attribute (RFC
// 3339 with fractional seconds, e.g. "2026-06-01T12:00:00.1234567Z")
// into a time.Time. Returns time.Now() on parse failure so a record is
// never dropped solely for an unparseable timestamp.
func parseSystemTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}

// winLevelToText maps the Windows Event Log level (1..5) to a stable
// short label used in the OTel SeverityText field and for the operator-
// facing `levels:` config filter.
//
//	1 Critical
//	2 Error
//	3 Warning
//	4 Information
//	5 Verbose
//	0 LogAlways (treated as Information)
func winLevelToText(level int) string {
	switch level {
	case 1:
		return "Critical"
	case 2:
		return "Error"
	case 3:
		return "Warning"
	case 4:
		return "Information"
	case 5:
		return "Verbose"
	default:
		return "Information"
	}
}

// winLevelToSeverity maps the Windows level to the OTel SeverityNumber
// range (LogSeverity), aligning with the OTel Logs Data Model. Critical
// maps to FATAL (21) since it denotes an unrecoverable condition.
func winLevelToSeverity(level int) agentstate.LogSeverity {
	switch level {
	case 1:
		return agentstate.LogSeverityFatal
	case 2:
		return agentstate.LogSeverityError
	case 3:
		return agentstate.LogSeverityWarn
	case 4:
		return agentstate.LogSeverityInfo
	case 5:
		return agentstate.LogSeverityDebug
	default:
		return agentstate.LogSeverityInfo
	}
}

// toLogRecord converts a parsed (and already filtered) event into the
// agent-internal LogRecord envelope. Attribute names follow the issue
// #154 mandated keys (event_id, event_source, event_level,
// event_channel, event_provider, record_id) plus OTel-aligned host/
// process/user keys where one exists.
func (e parsedEvent) toLogRecord(probeName string, redactPII bool) agentstate.LogRecord {
	attrs := map[string]string{
		"event_id":       strconv.Itoa(e.EventID),
		"event_level":    winLevelToText(e.Level),
		"event_channel":  e.Channel,
		"event_provider": e.Provider,
		"event_source":   e.Provider,
		"record_id":      e.RecordID,
	}
	if e.Computer != "" {
		attrs["host.name"] = e.Computer
	}
	if e.ProcessID != "" {
		attrs["process.pid"] = e.ProcessID
	}
	if e.UserID != "" && !redactPII {
		attrs["user.id"] = e.UserID
	}
	for k, v := range e.EventData {
		if redactPII && isSensitiveField(k) {
			attrs["eventdata."+k] = redactedPlaceholder
			continue
		}
		attrs["eventdata."+k] = v
	}

	body := e.Message
	if redactPII {
		body = redactSecurityBody(e.Channel, body)
	}

	return agentstate.LogRecord{
		Timestamp:         e.Timestamp,
		Severity:          winLevelToSeverity(e.Level),
		SeverityText:      winLevelToText(e.Level),
		Body:              body,
		Attributes:        attrs,
		ProducerProbeName: probeName,
		ProducerProbeType: ProbeType,
	}
}

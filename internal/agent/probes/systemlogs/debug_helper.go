package systemlogs

import (
	"encoding/json"
	"fmt"
	"senhub-agent.go/internal/agent/windows/eventlog"
)

// eventLevelToString converts EventLevel to string
func eventLevelToString(level eventlog.EventLevel) string {
	switch level {
	case 0:
		return "LogAlways"
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
		return fmt.Sprintf("Level%d", level)
	}
}

// LogDetailedEventData logs detailed debug information about a Windows event
// This function should be called with a debug level logger to capture all event data
func LogDetailedEventData(event eventlog.Event, index int) map[string]interface{} {
	// Create a map to hold all the event data for detailed logging
	data := map[string]interface{}{
		"index":         index,
		"event_id":      event.EventID,
		"record_id":     event.EventRecordID,
		"provider":      event.ProviderName,
		"provider_guid": event.ProviderGUID,
		"level_raw":     uint8(event.Level),
		"level":         eventLevelToString(event.Level),
		"full_message":  event.Message,
		"timestamp":     event.TimeCreated.Format("2006-01-02T15:04:05.000Z07:00"),
		"computer":      event.Computer,
		"channel":       event.Channel,
	}

	// Add all data fields from the event
	for k, v := range event.Data {
		data["data_"+k] = v
	}

	// Add XML content if available
	if event.RawXML != "" && len(event.RawXML) < 5000 {
		data["raw_xml"] = event.RawXML
	} else if event.RawXML != "" {
		data["raw_xml"] = "[XML too large, first 2000 chars]:" + event.RawXML[:2000] + "..."
	}

	return data
}

// DumpEventToJSON converts an event to a JSON string for debugging
func DumpEventToJSON(event eventlog.Event) string {
	data := LogDetailedEventData(event, 0)
	
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling event to JSON: %v", err)
	}
	
	return string(jsonBytes)
}
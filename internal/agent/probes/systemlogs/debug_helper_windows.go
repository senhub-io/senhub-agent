//go:build windows

package systemlogs

import (
	"encoding/json"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/windows/eventlog"
)

// EnableUltraDetailedDebug activates the ultra-detailed debug logging for Windows Event subscriptions
func EnableUltraDetailedDebug() {
	// Enable the detailed logging in the eventlog package
	eventlog.UltraDebugLogEnabled = true
	eventlog.UltraDebugInit()
	
	// Log about the test program
	eventlog.UltraDebugLog("DEBUGGING_TIPS", map[string]interface{}{
		"message": "If you're still having trouble with Windows events, try our standalone test program",
		"location": "/cmd/winevent_test/",
		"usage": "winevent_test.exe --channels Application,System --future-only=true",
		"note": "Run as Administrator",
		"build": "Use build.bat to compile",
	})
}

// LogWindowsEventDetailed writes an event to the ultra-detailed debug log
func LogWindowsEventDetailed(eventType string, event eventlog.Event, source string) {
	// Check if debug logging is enabled
	if !eventlog.UltraDebugLogEnabled {
		return
	}

	// Create a data map with the important fields
	data := map[string]interface{}{
		"source":      source,
		"provider":    event.ProviderName,
		"event_id":    event.EventID,
		"level":       event.Level,
		"level_name":  event.Level.String(),
		"channel":     event.Channel,
		"computer":    event.Computer,
		"timestamp":   event.TimeCreated.Format(time.RFC3339Nano),
		"record_id":   event.EventRecordID,
		"message_len": len(event.Message),
		"data_fields": len(event.Data),
	}

	// Add message truncated if it's too long
	if len(event.Message) > 200 {
		data["message"] = event.Message[:200] + "... [truncated]"
	} else {
		data["message"] = event.Message
	}

	// Log to the ultra-detailed log
	eventlog.UltraDebugLog(eventType, data)
}

// DumpEventToJSONDetailed converts an event to more detailed JSON for Windows-specific debugging
func DumpEventToJSONDetailed(event eventlog.Event) string {
	// Create a simplified representation for logging
	simpleEvent := map[string]interface{}{
		"event_id":      event.EventID,
		"provider":      event.ProviderName,
		"level":         event.Level,
		"level_name":    event.Level.String(),
		"record_id":     event.EventRecordID,
		"channel":       event.Channel,
		"computer":      event.Computer,
		"time_created":  event.TimeCreated,
		"process_id":    event.ProcessID,
		"thread_id":     event.ThreadID,
		"message":       event.Message,
		"data":          event.Data,
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(simpleEvent, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling event: %v", err)
	}

	return string(jsonData)
}
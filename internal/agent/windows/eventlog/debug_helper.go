//go:build windows

package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
	
	"golang.org/x/sys/windows"
)

// UltraDebugLogEnabled controls whether ultra-detailed logging is enabled
var UltraDebugLogEnabled = true

// UltraDebugLogFile is the path to the ultra-detailed log file
var UltraDebugLogFile = ""

// UltraDebugJSON controls whether to output logs as JSON (true) or as plain text (false)
var UltraDebugJSON = true

// GetErrorCode extracts a uint32 error code from an error interface, defaulting to 0 if conversion fails
func GetErrorCode(err error) uint32 {
	if err == nil {
		return 0
	}
	// Try to convert to windows.Errno
	if errVal, ok := err.(windows.Errno); ok {
		return uint32(errVal)
	}
	return 0
}

// UltraDebugInit initializes the ultra-detailed logging system
func UltraDebugInit() {
	// Initialize the log file if it's not set
	if UltraDebugLogFile == "" {
		// Get executable directory or temp directory
		exePath, err := os.Executable()
		var basePath string
		if err != nil {
			basePath = os.TempDir()
		} else {
			basePath = filepath.Dir(exePath)
		}

		// Create logs subdirectory if needed
		logsDir := filepath.Join(basePath, "logs")
		os.MkdirAll(logsDir, 0755)

		// Create the log file with timestamp
		timestamp := time.Now().Format("20060102-150405")
		UltraDebugLogFile = filepath.Join(logsDir, fmt.Sprintf("winevents-debug-%s.log", timestamp))
	}

	// Log startup message
	UltraDebugLog("ULTRA_DEBUG_STARTUP", map[string]interface{}{
		"file":      UltraDebugLogFile,
		"timestamp": time.Now().Format(time.RFC3339),
		"platform":  runtime.GOOS,
		"pid":       os.Getpid(),
	})
}

// UltraDebugLog logs an ultra-detailed debug message to the log file
func UltraDebugLog(eventType string, data map[string]interface{}) {
	if !UltraDebugLogEnabled {
		return
	}

	// Initialize if needed
	if UltraDebugLogFile == "" {
		UltraDebugInit()
	}

	// Create the log entry
	entry := map[string]interface{}{
		"type":      eventType,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}

	// Add all data fields
	for k, v := range data {
		entry[k] = v
	}

	// Open the log file for appending
	file, err := os.OpenFile(UltraDebugLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// If we can't open the log file, fall back to console
		fmt.Printf("UltraDebugLog ERROR: Cannot open log file: %v\n", err)
		return
	}
	defer file.Close()

	// Format the log entry
	var logLine string
	if UltraDebugJSON {
		// JSON format
		jsonData, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			fmt.Printf("UltraDebugLog ERROR: Cannot marshal JSON: %v\n", err)
			return
		}
		logLine = string(jsonData) + "\n"
	} else {
		// Plain text format
		logLine = fmt.Sprintf("[%s] %s: ", entry["timestamp"], entry["type"])
		for k, v := range data {
			logLine += fmt.Sprintf("%s=%v ", k, v)
		}
		logLine += "\n"
	}

	// Write to the log file
	if _, err := file.WriteString(logLine); err != nil {
		fmt.Printf("UltraDebugLog ERROR: Cannot write to log file: %v\n", err)
	}
}

// UltraDebugLogEvent logs detailed information about a Windows event
func UltraDebugLogEvent(event Event, source string) {
	if !UltraDebugLogEnabled {
		return
	}

	// Convert event data to a map
	eventData := make(map[string]interface{})
	for k, v := range event.Data {
		eventData[k] = v
	}

	// Create a data structure for logging
	data := map[string]interface{}{
		"source":       source,
		"event_id":     event.EventID,
		"provider":     event.ProviderName,
		"level":        event.Level,
		"record_id":    event.EventRecordID,
		"channel":      event.Channel,
		"computer":     event.Computer,
		"time_created": event.TimeCreated.Format(time.RFC3339),
		"data":         eventData,
	}

	// Add message but truncate if too long to avoid giant log files
	if len(event.Message) > 1000 {
		data["message"] = event.Message[:1000] + "... [truncated]"
	} else {
		data["message"] = event.Message
	}

	// Add a small portion of XML for debugging if needed
	if event.RawXML != "" && len(event.RawXML) > 0 {
		if len(event.RawXML) > 500 {
			data["xml_snippet"] = event.RawXML[:500] + "... [truncated]"
		} else {
			data["xml_snippet"] = event.RawXML
		}
	}

	// Log the event
	UltraDebugLog("EVENT_DATA", data)
}

// LogSubscriptionDetails logs detailed information about a subscription
func LogSubscriptionDetails(channel string, checkpoint *Checkpoint, flags uint32) {
	flagNames := map[uint32]string{
		EvtSubscribeToFutureEvents:   "EvtSubscribeToFutureEvents",
		EvtSubscribeStartAtOldestRecord: "EvtSubscribeStartAtOldestRecord",
		EvtSubscribeStartAfterBookmark: "EvtSubscribeStartAfterBookmark",
	}
	
	flagName, ok := flagNames[flags]
	if !ok {
		flagName = fmt.Sprintf("Unknown(%d)", flags)
	}
	
	data := map[string]interface{}{
		"channel":    channel,
		"flags":      flags,
		"flags_name": flagName,
	}
	
	if checkpoint != nil {
		data["checkpoint_position"] = checkpoint.Position
		data["checkpoint_timestamp"] = checkpoint.Timestamp
		data["checkpoint_filter"] = checkpoint.Filter
		
		if len(checkpoint.BookmarkXML) > 0 {
			if len(checkpoint.BookmarkXML) > 200 {
				data["bookmark_xml"] = checkpoint.BookmarkXML[:200] + "... [truncated]"
			} else {
				data["bookmark_xml"] = checkpoint.BookmarkXML
			}
		}
	}
	
	UltraDebugLog("SUBSCRIPTION_DETAILS", data)
}

// LogWindowsError logs detailed information about a Windows error
func LogWindowsError(operation string, err error, errorCode uint32) {
	// Map of common Windows error codes to their meanings
	errorCodes := map[uint32]string{
		ERROR_SUCCESS:              "ERROR_SUCCESS",
		ERROR_INSUFFICIENT_BUFFER:  "ERROR_INSUFFICIENT_BUFFER",
		ERROR_NO_MORE_ITEMS:        "ERROR_NO_MORE_ITEMS",
		ERROR_EVT_MESSAGE_NOT_FOUND: "ERROR_EVT_MESSAGE_NOT_FOUND",
		ERROR_EVT_CHANNEL_NOT_FOUND: "ERROR_EVT_CHANNEL_NOT_FOUND",
		ERROR_EVT_INVALID_CHANNEL_PATH: "ERROR_EVT_INVALID_CHANNEL_PATH",
		ERROR_ACCESS_DENIED:        "ERROR_ACCESS_DENIED",
		ERROR_INVALID_HANDLE:       "ERROR_INVALID_HANDLE",
		ERROR_INVALID_PARAMETER:    "ERROR_INVALID_PARAMETER",
		ERROR_RPC_S_SERVER_UNAVAILABLE: "ERROR_RPC_S_SERVER_UNAVAILABLE",
		ERROR_RPC_S_CALL_CANCELLED:     "ERROR_RPC_S_CALL_CANCELLED",
		ERROR_EVT_QUERY_RESULT_STALE:   "ERROR_EVT_QUERY_RESULT_STALE",
		ERROR_EVT_PUBLISHER_DISABLED:   "ERROR_EVT_PUBLISHER_DISABLED",
	}
	
	errorName, ok := errorCodes[errorCode]
	if !ok {
		errorName = fmt.Sprintf("Unknown(%d)", errorCode)
	}
	
	data := map[string]interface{}{
		"operation":   operation,
		"error":       err,
		"error_code":  errorCode,
		"error_name":  errorName,
		"error_hex":   fmt.Sprintf("0x%x", errorCode),
	}
	
	// Add error message if available
	if err != nil {
		data["error_message"] = err.Error()
	}
	
	UltraDebugLog("WINDOWS_ERROR", data)
}
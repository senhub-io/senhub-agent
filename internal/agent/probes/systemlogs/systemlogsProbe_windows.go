//go:build windows
// +build windows

package systemlogs

import (
	"context"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/windows/eventlog"
)

// Windows Event Log specific constants
const (
	EvtSubscribeToFutureEvents uint32 = 1
	EvtSubscribeStartAtOldestRecord uint32 = 2
	
	EvtRenderEventValues uint32 = 0
	EvtRenderEventXml uint32 = 1
	
	EvtFormatMessageEvent uint32 = 1
	
	EvtSystemProviderName uint32 = 2
	EvtSystemEventID uint32 = 7
	EvtSystemLevel uint32 = 8
	EvtSystemChannel uint32 = 11
	EvtSystemComputer uint32 = 12
	EvtSystemTimeCreated uint32 = 14
)

// Handle is a Windows handle.
type EvtHandle uintptr

// Windows API functions needed for event log subscriptions
var (
	modwevtapi                  = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtSubscribe            = modwevtapi.NewProc("EvtSubscribe")
	procEvtRender               = modwevtapi.NewProc("EvtRender")
	procEvtClose                = modwevtapi.NewProc("EvtClose")
	procEvtNext                 = modwevtapi.NewProc("EvtNext")
	procEvtCreateRenderContext  = modwevtapi.NewProc("EvtCreateRenderContext")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtFormatMessage        = modwevtapi.NewProc("EvtFormatMessage")
	procEvtOpenSession          = modwevtapi.NewProc("EvtOpenSession")
	procEvtQuery                = modwevtapi.NewProc("EvtQuery")
	
	// Additional kernel32 functions
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	procFileTimeToSystemTime = modkernel32.NewProc("FileTimeToSystemTime")
	procSystemTimeToTzSpecificLocalTime = modkernel32.NewProc("SystemTimeToTzSpecificLocalTime")
)

// LevelNameMap maps Windows event levels to string names
var LevelNameMap = map[uint8]string{
	0:  "LogAlways",  // Level 0
	1:  "Critical",   // Level 1
	2:  "Error",      // Level 2
	3:  "Warning",    // Level 3
	4:  "Information", // Level 4
	5:  "Verbose",    // Level 5
}

// SYSTEMTIME represents a Windows SYSTEMTIME structure
type SYSTEMTIME struct {
	Year         uint16
	Month        uint16
	DayOfWeek    uint16
	Day          uint16
	Hour         uint16
	Minute       uint16
	Second       uint16
	Milliseconds uint16
}

// FILETIME represents a Windows FILETIME structure
type FILETIME struct {
	LowDateTime  uint32
	HighDateTime uint32
}

// WindowsEvent represents a Windows Event Log entry
type WindowsEvent struct {
	Channel   string
	Provider  string
	ID        int
	Level     string
	Message   string
	Timestamp time.Time
	Computer  string
}

// isSourceSupported checks if a log source is supported on Windows
func isSourceSupported(source LogSource) bool {
	return source == LogSourceWindowsEvent
}

// evtClose closes an open event handle
func evtClose(handle EvtHandle) error {
	ret, _, _ := procEvtClose.Call(uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("EvtClose failed")
	}
	return nil
}

// evtQuery executes a query to retrieve events
func evtQuery(path string, query string) (EvtHandle, error) {
	var flags uint32 = 1 // EvtQueryChannelPath
	
	// Convert strings to UTF16
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	
	queryPtr, err := windows.UTF16PtrFromString(query)
	if err != nil {
		return 0, err
	}
	
	// Call EvtQuery
	ret, _, err := procEvtQuery.Call(
		0, // Session (NULL = local)
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(queryPtr)),
		uintptr(flags),
	)
	
	if ret == 0 {
		return 0, fmt.Errorf("EvtQuery failed: %v", err)
	}
	
	return EvtHandle(ret), nil
}

// evtNext gets the next event from the result set
func evtNext(resultSet EvtHandle, maxEvents uint32) ([]EvtHandle, error) {
	eventHandles := make([]EvtHandle, maxEvents)
	var numReturned uint32
	
	ret, _, err := procEvtNext.Call(
		uintptr(resultSet),
		uintptr(maxEvents),
		uintptr(unsafe.Pointer(&eventHandles[0])),
		0,
		0,
		uintptr(unsafe.Pointer(&numReturned)),
	)
	
	if ret == 0 {
		if err != nil && err != windows.ERROR_NO_MORE_ITEMS {
			return nil, fmt.Errorf("EvtNext failed: %v", err)
		}
		return nil, nil // No more items
	}
	
	return eventHandles[:numReturned], nil
}

// evtRender renders an event for consumption
func evtRender(eventHandle EvtHandle, flag uint32) ([]byte, error) {
	var bufferUsed, propertyCount uint32
	
	// First call to get required buffer size
	ret, _, _ := procEvtRender.Call(
		0, // Context (NULL for EvtRenderEventXml)
		uintptr(eventHandle),
		uintptr(flag),
		0, // BufferSize
		0, // Buffer
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		// Check if the error is just that we need a bigger buffer
		lastError := windows.GetLastError()
		if lastError != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, fmt.Errorf("EvtRender failed: %v", lastError)
		}
	}
	
	// Allocate buffer of required size
	buffer := make([]byte, bufferUsed)
	
	// Second call with properly sized buffer
	ret, _, err := procEvtRender.Call(
		0, // Context
		uintptr(eventHandle),
		uintptr(flag),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return nil, fmt.Errorf("EvtRender failed with sized buffer: %v", err)
	}
	
	return buffer[:bufferUsed], nil
}

// evtFormatMessage formats an event message
func evtFormatMessage(publisherMetadata EvtHandle, eventHandle EvtHandle, messageID uint32) (string, error) {
	var bufferUsed uint32
	
	// First call to get required buffer size
	ret, _, _ := procEvtFormatMessage.Call(
		uintptr(publisherMetadata),
		uintptr(eventHandle),
		0, // MessageId
		0, // ValueCount
		0, // Values
		uintptr(EvtFormatMessageEvent),
		0, // BufferSize
		0, // Buffer
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 {
		// Check if the error is just that we need a bigger buffer
		lastError := windows.GetLastError()
		if lastError != windows.ERROR_INSUFFICIENT_BUFFER {
			return "", fmt.Errorf("EvtFormatMessage failed: %v", lastError)
		}
	}
	
	// Allocate buffer of required size (2 bytes per wchar)
	buffer := make([]uint16, bufferUsed)
	
	// Second call with properly sized buffer
	ret, _, err := procEvtFormatMessage.Call(
		uintptr(publisherMetadata),
		uintptr(eventHandle),
		0, // MessageId
		0, // ValueCount
		0, // Values
		uintptr(EvtFormatMessageEvent),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 {
		// Check for access denied or resource not found, which are common
		lastError := windows.GetLastError()
		if lastError == windows.ERROR_ACCESS_DENIED {
			return "[Access Denied to Message Text]", nil
		}
		if lastError == windows.ERROR_EVT_MESSAGE_NOT_FOUND {
			return "[Message Resource Not Found]", nil
		}
		return "", fmt.Errorf("EvtFormatMessage failed with sized buffer: %v", err)
	}
	
	// Convert UTF16 to string (removing null terminator)
	return windows.UTF16ToString(buffer), nil
}

// evtOpenPublisherMetadata opens publisher metadata
func evtOpenPublisherMetadata(publisherName string) (EvtHandle, error) {
	publisherNamePtr, err := windows.UTF16PtrFromString(publisherName)
	if err != nil {
		return 0, err
	}
	
	ret, _, err := procEvtOpenPublisherMetadata.Call(
		0, // Session (NULL = local)
		uintptr(unsafe.Pointer(publisherNamePtr)),
		0, // Locale (NULL = current)
		0, // Flags
	)
	
	if ret == 0 {
		return 0, fmt.Errorf("EvtOpenPublisherMetadata failed: %v", err)
	}
	
	return EvtHandle(ret), nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateString truncates a string to the given maximum length
// and adds an ellipsis if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// fileTimeToSystemTime converts FILETIME to SYSTEMTIME
func fileTimeToSystemTime(fileTime FILETIME) (SYSTEMTIME, error) {
	var systemTime SYSTEMTIME
	
	ret, _, err := procFileTimeToSystemTime.Call(
		uintptr(unsafe.Pointer(&fileTime)),
		uintptr(unsafe.Pointer(&systemTime)),
	)
	
	if ret == 0 {
		return systemTime, fmt.Errorf("FileTimeToSystemTime failed: %v", err)
	}
	
	return systemTime, nil
}

// fileTimeToTime converts a Windows FILETIME to Go time.Time
func fileTimeToTime(fileTime FILETIME) (time.Time, error) {
	// First convert FILETIME to SYSTEMTIME
	systemTime, err := fileTimeToSystemTime(fileTime)
	if err != nil {
		return time.Time{}, err
	}
	
	// Convert to time.Time
	t := time.Date(
		int(systemTime.Year),
		time.Month(systemTime.Month),
		int(systemTime.Day),
		int(systemTime.Hour),
		int(systemTime.Minute),
		int(systemTime.Second),
		int(systemTime.Milliseconds)*1000000, // milliseconds to nanoseconds
		time.Local,
	)
	
	return t, nil
}

// getEventProperty extracts a specific property from rendered event data
func getEventProperty(renderedData []byte, propertyID uint32) ([]byte, error) {
	// Windows Event properties use an array of EVT_VARIANT structures
	// Each EVT_VARIANT is 16 bytes: 8 bytes for the value (or pointer) and 8 bytes for type/flags
	
	// Calculate offset into the rendered data for this property
	// Each property is at propertyID * sizeof(EVT_VARIANT)
	offset := propertyID * 16
	
	// Check if we have enough data
	if len(renderedData) < int(offset+16) {
		return nil, fmt.Errorf("renderedData too small for property %d", propertyID)
	}
	
	// Extract 8 bytes containing the property value or pointer
	// Note: We're simplifying here by ignoring the type information in the second 8 bytes
	// A complete implementation would check the type and handle it accordingly
	
	// The first 8 bytes of an EVT_VARIANT contain the value
	return renderedData[offset : offset+8], nil
}

// buildWindowsEventQuery creates an XPath query for Windows events based on filters
func buildWindowsEventQuery(eventIDs []int, levels []string, since time.Time, logger *logger.Logger) string {
	// Start with basic query structure
	query := "*"
	
	// Add filters if specified
	if len(eventIDs) > 0 || len(levels) > 0 || !since.IsZero() {
		query = "*[System["
		
		// Add event ID filter
		if len(eventIDs) > 0 {
			if len(eventIDs) == 1 {
				query += fmt.Sprintf("(EventID=%d)", eventIDs[0])
			} else {
				query += "("
				for i, id := range eventIDs {
					if i > 0 {
						query += " or "
					}
					query += fmt.Sprintf("EventID=%d", id)
				}
				query += ")"
			}
		}
		
		// Add level filter
		if len(levels) > 0 {
			if len(eventIDs) > 0 {
				query += " and "
			}
			
			query += "("
			for i, level := range levels {
				if i > 0 {
					query += " or "
				}
				
				// Map level strings to numeric values
				var levelNum int
				switch level {
				case "Critical":
					levelNum = 1
				case "Error":
					levelNum = 2
				case "Warning":
					levelNum = 3
				case "Information":
					levelNum = 4
				case "Verbose":
					levelNum = 5
				default:
					continue
				}
				
				query += fmt.Sprintf("Level=%d", levelNum)
			}
			query += ")"
		}
		
		// Add time filter
		if !since.IsZero() {
			if len(eventIDs) > 0 || len(levels) > 0 {
				query += " and "
			}
			
			// Format time as required by Windows Event Log query
			timeStr := since.Format("2006-01-02T15:04:05.000Z")
			query += fmt.Sprintf("TimeCreated[@SystemTime>='%s']", timeStr)
		}
		
		query += "]]"
	}
	
	return query
}

// queryWindowsEvents retrieves events from a Windows Event Log channel
func queryWindowsEvents(channel, query string, maxEvents int) ([]WindowsEvent, error) {
	events := []WindowsEvent{}
	
	// Create query handle
	queryHandle, err := evtQuery(channel, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %v", err)
	}
	defer evtClose(queryHandle)
	
	// Fetch events in batches (maximum 10 at a time for memory management)
	batchSize := uint32(10)
	if uint32(maxEvents) < batchSize {
		batchSize = uint32(maxEvents)
	}
	
	eventsCollected := 0
	
	for eventsCollected < maxEvents {
		// Get next batch of events
		eventHandles, err := evtNext(queryHandle, batchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to get next events: %v", err)
		}
		
		// Break if no more events
		if eventHandles == nil || len(eventHandles) == 0 {
			break
		}
		
		// Process each event
		for _, eventHandle := range eventHandles {
			// Skip invalid handles
			if eventHandle == 0 {
				continue
			}
			
			// Clean up event handle when done
			defer evtClose(eventHandle)
			
			// Get event values
			eventValues, err := evtRender(eventHandle, EvtRenderEventValues)
			if err != nil {
				continue // Skip events that fail to render
			}
			
			// Extract provider name
			providerBytes, err := getEventProperty(eventValues, EvtSystemProviderName)
			var providerName string
			if err != nil || len(providerBytes) < 8 {
				providerName = "Unknown Provider"
			} else {
				// The provider name is stored as a pointer to a UTF16 string
				// First extract the pointer value from bytes
				providerPtr := *(*uintptr)(unsafe.Pointer(&providerBytes[0]))
				if providerPtr != 0 {
					// Convert to string - Get length of null-terminated string
					for i := 0; ; i += 2 {
						if *(*uint16)(unsafe.Pointer(providerPtr + uintptr(i))) == 0 {
							// Convert to Go string
							providerName = windows.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(providerPtr))[:i/2])
							break
						}
					}
				} else {
					providerName = "Unknown Provider"
				}
			}

			// Extract channel - using parameter but also try from property
			channelName := channel 
			channelBytes, err := getEventProperty(eventValues, EvtSystemChannel)
			if err == nil && len(channelBytes) >= 8 {
				// Extract channel name similar to provider
				channelPtr := *(*uintptr)(unsafe.Pointer(&channelBytes[0]))
				if channelPtr != 0 {
					for i := 0; ; i += 2 {
						if *(*uint16)(unsafe.Pointer(channelPtr + uintptr(i))) == 0 {
							// Convert to Go string
							extractedChannel := windows.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(channelPtr))[:i/2])
							if extractedChannel != "" {
								channelName = extractedChannel
							}
							break
						}
					}
				}
			}

			// Extract event ID
			eventIDBytes, err := getEventProperty(eventValues, EvtSystemEventID)
			eventID := 0
			if err == nil && len(eventIDBytes) >= 4 {
				// Event ID is typically a UINT32 value
				eventID = int(*(*uint32)(unsafe.Pointer(&eventIDBytes[0])))
			}

			// Extract level
			levelBytes, err := getEventProperty(eventValues, EvtSystemLevel)
			level := uint8(4) // Default to Information level
			if err == nil && len(levelBytes) >= 1 {
				// Level is typically a UINT8 value
				level = *(*uint8)(unsafe.Pointer(&levelBytes[0]))
			}
			levelName, ok := LevelNameMap[level]
			if !ok {
				levelName = "Unknown"
			}

			// Extract timestamp (stored as FILETIME)
			timeBytes, err := getEventProperty(eventValues, EvtSystemTimeCreated)
			timestamp := time.Now() // Default to current time if we can't parse
			if err == nil && len(timeBytes) >= 8 {
				// Convert FILETIME to time.Time
				ft := FILETIME{
					LowDateTime:  *(*uint32)(unsafe.Pointer(&timeBytes[0])),
					HighDateTime: *(*uint32)(unsafe.Pointer(&timeBytes[4])),
				}
				
				t, err := fileTimeToTime(ft)
				if err == nil {
					timestamp = t
				}
			}
			
			// Get event message
			var message string
			
			// Open publisher metadata to format message
			publisherHandle, err := evtOpenPublisherMetadata(providerName)
			if err == nil {
				defer evtClose(publisherHandle)
				
				// Format message
				message, err = evtFormatMessage(publisherHandle, eventHandle, 0)
				if err != nil {
					message = fmt.Sprintf("[Error formatting message: %v]", err)
				}
			} else {
				// If we can't get the publisher metadata, use a default message
				message = fmt.Sprintf("Event ID %d from %s", eventID, providerName)
			}
			
			// Extract computer name
			computerBytes, err := getEventProperty(eventValues, EvtSystemComputer)
			var computerName string = "Unknown"
			if err == nil && len(computerBytes) >= 8 {
				// Extract computer name similar to provider
				computerPtr := *(*uintptr)(unsafe.Pointer(&computerBytes[0]))
				if computerPtr != 0 {
					for i := 0; ; i += 2 {
						if *(*uint16)(unsafe.Pointer(computerPtr + uintptr(i))) == 0 {
							// Convert to Go string
							extractedComputer := windows.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(computerPtr))[:i/2])
							if extractedComputer != "" {
								computerName = extractedComputer
							}
							break
						}
					}
				}
			}
			
			// Create Windows event
			event := WindowsEvent{
				Channel:   channelName,
				Provider:  providerName,
				ID:        eventID,
				Level:     levelName,
				Message:   message,
				Timestamp: timestamp,
				Computer:  computerName,
			}
			
			events = append(events, event)
			eventsCollected++
			
			// Stop if we've reached the maximum
			if eventsCollected >= maxEvents {
				break
			}
		}
		
		// Break if we've reached the maximum
		if eventsCollected >= maxEvents {
			break
		}
	}
	
	return events, nil
}

// collectWindowsEvents collects Windows Event Log entries using the new eventlog package
func collectWindowsEvents(p *SystemLogsProbe) ([]SystemLogEvent, error) {
	// We use subscription-based model now, so this polling method is disabled
	p.logger.Info().Msg("Windows Event logs are collected via subscription model, not polling")
	return []SystemLogEvent{}, nil
}
// GetEventLevelName converts a level number to a readable string
func GetEventLevelName(level uint8) string {
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

// collectSystemLogs is the Windows implementation that collects from supported sources
func collectSystemLogs(p *SystemLogsProbe) ([]data_store.DataPoint, error) {
	dataPoints := []data_store.DataPoint{}
	
	// Collect from each configured source
	for _, source := range p.config.Sources {
		switch source {
		case LogSourceWindowsEvent:
			events, err := collectWindowsEvents(p)
			if err != nil {
				p.logger.Error().Err(err).Str("source", string(source)).Msg("Error collecting Windows events")
				continue
			}
			
			// Convert events to data points
			for _, event := range events {
				dataPoint := p.ProcessEvent(event)
				dataPoints = append(dataPoints, dataPoint)
			}
		default:
			p.logger.Debug().Str("source", string(source)).Msg("Skipping unsupported log source on Windows")
		}
	}
	
	// Update the last collection time
	p.lastCollection = time.Now()
	
	p.logger.Info().Int("count", len(dataPoints)).Msg("Collected system log entries")
	return dataPoints, nil
}

// startSystemLogSubscriptions is the Windows implementation of OnStart
func startSystemLogSubscriptions(p *SystemLogsProbe, quitChannel chan struct{}) error {
	p.logger.Info().Msg("🚀 Starting System Logs probe with Windows Event Subscription model")
	
	// Skip real-time subscription if WindowsEvent source is not enabled
	sourceEnabled := false
	for _, source := range p.config.Sources {
		if source == LogSourceWindowsEvent {
			sourceEnabled = true
			break
		}
	}
	
	if !sourceEnabled {
		p.logger.Info().Msg("Windows Event source not enabled, skipping subscription")
		return nil
	}
	
	// Log configured channels, event IDs, and levels
	p.logger.Info().
		Strs("channels", p.config.WindowsSettings.Channels).
		Ints("event_ids", p.config.WindowsSettings.EventIDs).
		Strs("levels", p.config.WindowsSettings.Levels).
		Msg("🎯 Windows Event Subscription Configuration")
		
	// Check if we have any filters that might exclude the event
	hasEventIDFilter := len(p.config.WindowsSettings.EventIDs) > 0
	hasLevelFilter := len(p.config.WindowsSettings.Levels) > 0
	
	// Log whether we have filtering that might exclude events
	p.logger.Info().
		Bool("has_event_id_filter", hasEventIDFilter).
		Bool("has_level_filter", hasLevelFilter).
		Msg("🔍 Checking if event filters might exclude your events")
	
	// Initialize EventLogManager if not already done
	if eventLogManager == nil {
		p.logger.Info().Msg("🔧 Initializing Windows Event Log manager for subscription")
		
		// Create a new manager with the configured channels
		manager, err := eventlog.NewManager(
			p.config.WindowsSettings.Channels,
			eventlog.WithDebug(false),
			eventlog.WithMaxEvents(p.config.MaxEvents),
			eventlog.WithIncludeXML(false), // Don't include raw XML by default
		)
		
		if err != nil {
			p.logger.Error().Err(err).Msg("❌ Failed to create Windows Event Log manager")
			return err
		}
		
		// Initialize the manager
		if err := manager.Init(); err != nil {
			p.logger.Error().Err(err).Msg("❌ Failed to initialize Windows Event Log manager")
			return err
		}
		
		p.logger.Info().
			Str("api", manager.GetCurrentAPI()).
			Msg("✅ Windows Event Log manager initialized successfully")
		
		eventLogManager = manager
	}
	
	// Start a goroutine to collect events in real-time
	go func() {
		p.logger.Info().Msg("📡 Starting real-time Windows Event Log subscription")
		
		// Create context that will be cancelled when quitChannel is closed
		ctx, cancel := context.WithCancel(context.Background())
		
		// Handle quit signal
		go func() {
			<-quitChannel
			cancel()
		}()
		
		// Start event collection
		p.logger.Info().Msg("⌛ Creating Windows Event subscriptions...")
		eventChan, errChan := eventLogManager.Start(ctx)
		p.logger.Info().Msg("✅ Windows Event subscriptions created and active")
		
		// Process events
		for {
			select {
			case batch, ok := <-eventChan:
				if !ok {
					p.logger.Info().Msg("Event channel closed, stopping real-time collection")
					return
				}
				
				if len(batch.Events) > 0 {
					p.logger.Info().
						Str("channel", batch.Channel).
						Int("count", len(batch.Events)).
						Msg("🔔 RECEIVED EVENTS FROM WINDOWS EVENT SUBSCRIPTION")

						// Log the first event's RAW JSON dump in debug level
						if len(batch.Events) > 0 {
							// Convert the first event to JSON and log it
							jsonDump := DumpEventToJSON(batch.Events[0])
							p.logger.Debug().
								Str("raw_event", jsonDump).
								Msg("🔬 RAW EVENT JSON DUMP (helpful for debugging)")
						}
						
					// Log informative message about filtering
					if len(p.config.WindowsSettings.EventIDs) > 0 || len(p.config.WindowsSettings.Levels) > 0 {
						p.logger.Info().
							Ints("configured_event_ids", p.config.WindowsSettings.EventIDs).
							Strs("configured_levels", p.config.WindowsSettings.Levels).
							Msg("⚠️ EVENT FILTERING ACTIVE - Events not matching these filters will be excluded")
					}
					
					// Log details of first few events to help debugging
					for i := 0; i < min(5, len(batch.Events)); i++ {
						evt := batch.Events[i]
						p.logger.Info().
							Int("index", i).
							Uint32("event_id", evt.EventID).
							Str("provider", evt.ProviderName).
							Str("level", GetEventLevelName(uint8(evt.Level))).
							Str("message", truncateString(evt.Message, 100)).
							Time("timestamp", evt.TimeCreated).
							Str("computer", evt.Computer).
							Msg("📋 EVENT DETAILS")
					}
					
					// Convert Windows events to SystemLogEvents
					events := make([]SystemLogEvent, 0, len(batch.Events))
					for _, evt := range batch.Events {
						// Apply event ID filter if configured
						if len(p.config.WindowsSettings.EventIDs) > 0 {
							found := false
							for _, id := range p.config.WindowsSettings.EventIDs {
								if int(evt.EventID) == id {
									found = true
									break
								}
							}
							if !found {
								continue
							}
						}
						
						// Apply level filter if configured
						if len(p.config.WindowsSettings.Levels) > 0 {
							levelName := GetEventLevelName(uint8(evt.Level))
							found := false
							for _, level := range p.config.WindowsSettings.Levels {
								if levelName == level {
									found = true
									break
								}
							}
							if !found {
								continue
							}
						}
						
						sysEvent := SystemLogEvent{
							Source:    evt.ProviderName,
							ID:        fmt.Sprintf("%d", evt.EventID),
							Level:     GetEventLevelName(uint8(evt.Level)),
							Message:   evt.Message,
							Timestamp: evt.TimeCreated,
							Metadata: map[string]string{
								"channel":  evt.Channel,
								"hostname": evt.Computer,
							},
						}
						
						// Add any additional metadata from event data
						for key, value := range evt.Data {
							// Skip metadata fields we've already added
							if key != "channel" && key != "hostname" {
								sysEvent.Metadata[key] = value
							}
						}
						
						events = append(events, sysEvent)
					}
					
					// Process events and send to callback if any passed filtering
					if len(events) > 0 {
						dataPoints := make([]data_store.DataPoint, 0, len(events))
						for _, event := range events {
							dataPoint := p.ProcessEvent(event)
							dataPoints = append(dataPoints, dataPoint)
						}
						
						// Send data points via callback
						if p.callback != nil && len(dataPoints) > 0 {
							p.logger.Debug().
								Int("count", len(dataPoints)).
								Msg("Sending real-time events to callback")
								
							if err := p.callback(dataPoints); err != nil {
								p.logger.Error().
									Err(err).
									Msg("Error in callback for real-time events")
							}
						}
					}
				}
				
			case err, ok := <-errChan:
				if !ok {
					p.logger.Info().Msg("Error channel closed, stopping real-time collection")
					return
				}
				
				p.logger.Error().Err(err).Msg("Error in real-time Windows Event collection")
				
			case <-ctx.Done():
				p.logger.Info().Msg("Context cancelled, stopping real-time collection")
				return
			}
		}
	}()
	
	return nil
}

// shutdownSystemLogSubscriptions is the Windows implementation of OnShutdown
func shutdownSystemLogSubscriptions(p *SystemLogsProbe, ctx context.Context) error {
	p.logger.Info().Msg("Stopping System Logs probe on Windows")
	
	// Save checkpoints and close the event log manager
	if eventLogManager != nil {
		p.logger.Debug().Msg("Saving Windows Event Log checkpoints")
		
		// Create a context with timeout to ensure we don't block shutdown indefinitely
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		
		// Save checkpoints with context
		var err error
		done := make(chan struct{})
		
		go func() {
			// Save checkpoints and close manager
			err = eventLogManager.SaveCheckpoints()
			if err != nil {
				p.logger.Error().Err(err).Msg("Error saving Windows Event Log checkpoints")
			}
			
			// Close the manager
			closeErr := eventLogManager.Close()
			if closeErr != nil && err == nil {
				err = closeErr
				p.logger.Error().Err(closeErr).Msg("Error closing Windows Event Log manager")
			}
			
			close(done)
		}()
		
		// Wait for completion or timeout
		select {
		case <-done:
			p.logger.Debug().Msg("Windows Event Log resources cleaned up")
		case <-ctx.Done():
			p.logger.Warn().Msg("Timeout waiting for Windows Event Log cleanup")
			return ctx.Err()
		}
		
		// Clear the global manager reference
		eventLogManager = nil
		
		return err
	}
	
	return nil
}

// EventLogManager provides access to the Windows Event Log API
var eventLogManager *eventlog.Manager

// cleanupResource is used for cleanup via runtime.SetFinalizer
type cleanupResource struct{}
var dummyResource = &cleanupResource{}

// Initialize platform-specific implementations
func init() {
	collectImpl = collectSystemLogs
	startImpl = startSystemLogSubscriptions
	shutdownImpl = shutdownSystemLogSubscriptions
	
	// Register for cleanup when the agent shuts down
	runtime.SetFinalizer(dummyResource, func(res *cleanupResource) {
		// Close any open handles
		if eventLogManager != nil {
			eventLogManager.Close()
			eventLogManager = nil
		}
	})
}

//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ModernAPI implements the modern Windows Event Log API (Vista+)
type ModernAPI struct {
	includeXML bool
	mutex      sync.Mutex
	cancelled  int32      // Atomic flag to indicate if operation is cancelled
}

// Windows constant definitions
const (
	EvtRenderContextValues   uint32 = 0
	EvtRenderContextSystem   uint32 = 1
	EvtRenderContextUser     uint32 = 2
	EvtQueryChannelPath      uint32 = 1
	EvtQueryFilePath         uint32 = 2
	EvtQueryForwardDirection uint32 = 0
	EvtQueryTolerateQueryErrors uint32 = 0x1000
)

// Name returns the API name
func (m *ModernAPI) Name() string {
	return "Modern Windows Event API (Vista+)"
}

// IsAvailable checks if the modern API is available
func (m *ModernAPI) IsAvailable() bool {
	// Check if we can load the wevtapi.dll library
	library := windows.NewLazySystemDLL("wevtapi.dll")
	err := library.Load()
	if err != nil {
		return false
	}
	
	// Try to find EvtOpenChannelEnum procedure
	proc := library.NewProc("EvtOpenChannelEnum")
	return proc != nil
}

// DLL and procedures needed for the Windows Event Log API
var (
	wevtapi                  = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtOpenChannelEnum   = wevtapi.NewProc("EvtOpenChannelEnum")
	procEvtClose             = wevtapi.NewProc("EvtClose")
	procEvtNextChannelPath   = wevtapi.NewProc("EvtNextChannelPath")
	procEvtOpenLog           = wevtapi.NewProc("EvtOpenLog")
	procEvtQuery             = wevtapi.NewProc("EvtQuery")
	procEvtNext              = wevtapi.NewProc("EvtNext")
	procEvtRender            = wevtapi.NewProc("EvtRender")
	procEvtCreateRenderContext = wevtapi.NewProc("EvtCreateRenderContext")
	procEvtFormatMessage     = wevtapi.NewProc("EvtFormatMessage")
	procEvtOpenPublisherMetadata = wevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtSubscribe         = wevtapi.NewProc("EvtSubscribe")
	
	// Kernel32 functions
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procFileTimeToSystemTime = kernel32.NewProc("FileTimeToSystemTime")
)

// IsRecoverableWindowsError identifies Windows Event Log errors that can be recovered from
func IsRecoverableWindowsError(errCode windows.Errno) bool {
	// Common Windows errors that can be retried
	switch errCode {
	case windows.ERROR_INVALID_HANDLE:
		return true
	case windows.Errno(ERROR_RPC_S_SERVER_UNAVAILABLE):
		return true
	case windows.Errno(ERROR_RPC_S_CALL_CANCELLED):
		return true
	case windows.Errno(ERROR_EVT_QUERY_RESULT_STALE):
		return true
	case windows.ERROR_INVALID_PARAMETER:
		return true
	case windows.Errno(ERROR_EVT_PUBLISHER_DISABLED):
		return true
	default:
		return false
	}
}

// Open opens a channel for reading
func (m *ModernAPI) Open(channel string) (windows.Handle, error) {
	// Convert channel string to UTF16
	channelUTF16, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return 0, fmt.Errorf("failed to convert channel name to UTF16: %w", err)
	}
	
	// Call EvtOpenLog to open the channel
	handle, _, err := procEvtOpenLog.Call(
		0, // Session (0 means local computer)
		uintptr(unsafe.Pointer(channelUTF16)),
		1, // EvtOpenChannelPath
	)
	
	if handle == 0 {
		// Get Windows error code and convert to Errno
		errCode, ok := windows.GetLastError().(windows.Errno)
		if !ok {
			errCode = 0
		}
		
		if IsRecoverableWindowsError(errCode) {
			return 0, fmt.Errorf("recoverable error opening event log channel '%s': %v", channel, errCode)
		}
		return 0, fmt.Errorf("failed to open event log channel '%s': %v", channel, errCode)
	}
	
	return windows.Handle(handle), nil
}

// Close closes a channel handle
func (m *ModernAPI) Close(handle windows.Handle) error {
	if handle == 0 {
		return nil
	}
	
	ret, _, err := procEvtClose.Call(uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("failed to close handle: %w", err)
	}
	
	return nil
}

// Maximum XML size to prevent memory issues
const MaxEventXmlSize = 1024 * 1024 // 1MB

// Read reads events from a channel
func (m *ModernAPI) Read(ctx context.Context, handle windows.Handle, maxEvents int, cp *Checkpoint) (*EventBatch, error) {
	// Reset the cancelled flag before starting
	atomic.StoreInt32(&m.cancelled, 0)
	
	// Create a safety channel for context timeout
	done := make(chan struct{})
	defer close(done)
	
	// Setup a goroutine to watch for context cancellation
	go func() {
		select {
		case <-ctx.Done():
			// Context was cancelled, set the atomic flag
			atomic.StoreInt32(&m.cancelled, 1)
		case <-done:
			// Function completed normally
		}
	}()
	
	// Mutex protection to ensure thread safety
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Check for invalid handle
	if handle == 0 {
		return nil, fmt.Errorf("invalid handle")
	}
	
	// Create a safer version that's fully protected against crashes
	var events []Event
	var highestPosition uint64
	var readErr error
	
	// Wrap the entire operation in a protected function to recover from panics
	func() {
		// Full protection against panics
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered from panic in Read function: %v\n", r)
				// Set an error to be returned
				readErr = fmt.Errorf("panic recovered in Read operation: %v", r)
			}
		}()
		
		// Create batch
		var batch EventBatch
		batch.Channel = cp.Channel
		
		// Create an array of event handles with a reasonable size
		if maxEvents <= 0 || maxEvents > 100 {
			maxEvents = 10 // Default to a safer size if invalid
		}
		eventHandles := make([]windows.Handle, maxEvents)
		var eventsReturned uint32
		
		// Call EvtNext to get event handles
		ret, _, _ := procEvtNext.Call(
			uintptr(handle),
			uintptr(maxEvents),
			uintptr(unsafe.Pointer(&eventHandles[0])),
			0, // Timeout (0 = return immediately)
			0, // Flags (0 = no special processing)
			uintptr(unsafe.Pointer(&eventsReturned)),
		)
		
		if ret == 0 {
			// Check the Windows error
			errCode, ok := windows.GetLastError().(windows.Errno)
			if !ok {
				errCode = 0
			}
			
			// ERROR_SUCCESS (0) or ERROR_NO_MORE_ITEMS both indicate no events found, which is normal
			if errCode == windows.ERROR_NO_MORE_ITEMS || errCode == 0 {
				// No events found, this is normal
				return
			}
			
			// Check if this is another recoverable error
			if IsRecoverableWindowsError(errCode) {
				// For recoverable errors, set an error for retry
				readErr = fmt.Errorf("recoverable error getting events (retry recommended): %v", errCode)
				return
			}
			
			// Other errors
			readErr = fmt.Errorf("failed to get events: %v", errCode)
			return
		}
		
		// Validate that we have sensible number of events returned
		if eventsReturned > uint32(maxEvents) {
			readErr = fmt.Errorf("invalid number of events returned: %d (max: %d)", eventsReturned, maxEvents)
			return
		}
		
		// Process each event
		for i := uint32(0); i < eventsReturned; i++ {
			// Check for context cancellation using the atomic flag
			if atomic.LoadInt32(&m.cancelled) == 1 {
				readErr = ctx.Err()
				// Close all event handles
				for j := uint32(0); j < eventsReturned; j++ {
					if eventHandles[j] != 0 {
						procEvtClose.Call(uintptr(eventHandles[j]))
					}
				}
				return
			}
			
			// Skip invalid handles
			if eventHandles[i] == 0 {
				continue
			}
			
			// Process this event in a protected sub-function
			func(eventHandle windows.Handle) {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("Recovered from panic processing event: %v\n", r)
					}
					
					// Always close the event handle to avoid leaks
					if eventHandle != 0 {
						procEvtClose.Call(uintptr(eventHandle))
					}
				}()
				
				// Try to get XML for the event first - this is the safest approach
				xmlData, _ := m.renderEventXml(eventHandle)
				
				// Create an event with defaults
				event := Event{
					Channel:     cp.Channel,
					ProviderName: "Unknown Provider",
					Computer:    "localhost",
					TimeCreated: time.Now(),
				}
				
				// If we have XML, extract all properties from it (safer than direct API calls)
				if xmlData != "" {
					// Store raw XML if needed
					event.RawXML = xmlData
					
					// Extract all relevant fields from XML
					if provider := m.extractFromXml(xmlData, "Provider Name=\"", "\""); provider != "" {
						event.ProviderName = provider
					}
					
					if eventID := m.extractFromXml(xmlData, "<EventID>", "</EventID>"); eventID != "" {
						if id, err := strconv.ParseUint(strings.TrimSpace(eventID), 10, 32); err == nil {
							event.EventID = uint32(id)
						}
					}
					
					if levelStr := m.extractFromXml(xmlData, "<Level>", "</Level>"); levelStr != "" {
						if level, err := strconv.ParseUint(strings.TrimSpace(levelStr), 10, 8); err == nil {
							event.Level = EventLevel(level)
						}
					}
					
					if computer := m.extractFromXml(xmlData, "<Computer>", "</Computer>"); computer != "" {
						event.Computer = computer
					}
					
					if timeStr := m.extractFromXml(xmlData, "SystemTime=\"", "\""); timeStr != "" {
						if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
							event.TimeCreated = t
						}
					}
					
					if channel := m.extractFromXml(xmlData, "<Channel>", "</Channel>"); channel != "" {
						event.Channel = channel
					}
					
					if recordIDStr := m.extractFromXml(xmlData, "<EventRecordID>", "</EventRecordID>"); recordIDStr != "" {
						if recordID, err := strconv.ParseUint(strings.TrimSpace(recordIDStr), 10, 64); err == nil {
							event.EventRecordID = recordID
							
							// Update highest position
							if recordID > highestPosition {
								highestPosition = recordID
							}
						}
					}
					
					// Extract event message if available in XML
					message := m.extractFromXml(xmlData, "<Message>", "</Message>")
					if message != "" {
						event.Message = message
					} else {
						// Try to get a formatted message using the provider name
						safeGetMessage := func() string {
							defer func() {
								if r := recover(); r != nil {
									// Just recover and return empty on panic
								}
							}()
							return m.getEventMessage(eventHandle, event.ProviderName)
						}
						
						event.Message = safeGetMessage()
					}
					
					// Extract data fields from EventData if present
					event.Data = make(map[string]string)
					eventData := m.extractFromXml(xmlData, "<EventData>", "</EventData>")
					if eventData != "" {
						// Parse data items
						dataItems := strings.Split(eventData, "<Data")
						for _, item := range dataItems[1:] { // Skip first element which is empty
							nameAttr := m.extractFromXml(item, "Name=\"", "\"")
							value := m.extractFromXml(item, ">", "</Data>")
							
							if nameAttr != "" {
								event.Data[nameAttr] = value
							} else if value != "" {
								// Unnamed data item
								event.Data[fmt.Sprintf("Param%d", len(event.Data)+1)] = value
							}
						}
					}
				} else {
					// Fallback to direct API calls if XML extraction failed
					// These are protected by individual function implementation
					event.ProviderName = m.getProviderName(eventHandle)
					event.EventID = m.getEventID(eventHandle)
					event.Level = m.getEventLevel(eventHandle)
					event.Computer = m.getComputerName(eventHandle)
					event.TimeCreated = m.getEventTime(eventHandle)
					event.Channel = m.getEventChannel(eventHandle)
					event.EventRecordID = m.getEventRecordID(eventHandle)
					event.Message = m.getEventMessage(eventHandle, event.ProviderName)
					
					// Update highest position
					if event.EventRecordID > highestPosition {
						highestPosition = event.EventRecordID
					}
				}
				
				// Add to events
				events = append(events, event)
			}(eventHandles[i])
		}
	}()
	
	// Check if we encountered an error
	if readErr != nil {
		return nil, readErr
	}
	
	// Update position for checkpoint
	if highestPosition > 0 {
		cp.Position = highestPosition
	}
	
	return &EventBatch{
		Channel:  cp.Channel,
		Events:   events,
		Position: cp.Position,
	}, nil
}

// Extract text between start and end markers from XML
func (m *ModernAPI) extractFromXml(xml, startMarker, endMarker string) string {
	startIdx := strings.Index(xml, startMarker)
	if startIdx == -1 {
		return ""
	}
	
	startIdx += len(startMarker)
	endIdx := strings.Index(xml[startIdx:], endMarker)
	if endIdx == -1 {
		return ""
	}
	
	return xml[startIdx : startIdx+endIdx]
}

// renderEventXml gets the XML representation of an event
// This implementation is based on the working code from winevent-agent
func (m *ModernAPI) renderEventXml(eventHandle windows.Handle) (string, error) {
	// Protection against invalid handle
	if eventHandle == 0 {
		return "", fmt.Errorf("invalid event handle")
	}
	
	// Check for cancellation first
	if atomic.LoadInt32(&m.cancelled) == 1 {
		return "", fmt.Errorf("operation cancelled")
	}
	
	// Wrap the entire function in a recover to prevent crashes
	var xmlContent string
	
	// Run in a protected function to recover from any panics
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered from panic in renderEventXml: %v\n", r)
			}
		}()
		
		// Multiple buffer sizes are tried to handle various event sizes - more conservative approach
		var bufferSizes = []uint32{4096, 8192, 16384, 32768, 65536} // Try progressively larger buffers
		
		// Try with progressively larger buffers
		for _, bufferSize := range bufferSizes {
			// Protect against memory allocation issues
			if bufferSize > MaxEventXmlSize {
				// Don't try buffers larger than our limit to avoid memory issues
				continue
			}
			
			// Initialize variables for this attempt
			var bufferUsed uint32
			var propertyCount uint32
			
			// Create a channel to monitor the operation to prevent hangs
			done := make(chan struct{})
			var buffer []byte
			var ret uintptr
			var errCode windows.Errno
			
			// Use a goroutine with timeout to allocate buffer and call API
			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("Recovered from nested panic in renderEventXml: %v\n", r)
					}
				}()
				
				// Allocate buffer with protection
				buffer = make([]byte, bufferSize)
				
				// Call EvtRender with the current buffer size - safe version with error checking
				ret, _, _ = procEvtRender.Call(
					0, // No context needed for XML rendering
					uintptr(eventHandle),
					uintptr(EvtRenderEventXml),
					uintptr(bufferSize),
					uintptr(unsafe.Pointer(&buffer[0])),
					uintptr(unsafe.Pointer(&bufferUsed)),
					uintptr(unsafe.Pointer(&propertyCount)),
				)
				
				// Capture error code
				errCode, _ = windows.GetLastError().(windows.Errno)
			}()
			
			// Wait for the operation to complete or timeout
			select {
			case <-done:
				// Operation completed
			case <-time.After(500 * time.Millisecond):
				// Operation timed out, try next buffer size
				fmt.Printf("Timeout in renderEventXml with buffer size %d\n", bufferSize)
				continue
			}
			
			// Check for success or specific error conditions
			if ret == 0 {
				// If buffer too small, try next size
				if errCode == windows.ERROR_INSUFFICIENT_BUFFER {
					continue
				}
				
				// For any other error, try next size but don't fail the whole operation
				continue
			}
			
			// Success case - validate buffer used size
			if buffer != nil && bufferUsed > 0 && bufferUsed <= bufferSize {
				// Don't try to process overly large buffers
				if bufferUsed > MaxEventXmlSize {
					fmt.Printf("Event XML too large: %d bytes\n", bufferUsed)
					continue
				}
				
				// Safer conversion with bounds checking
				// Get a slice of uint16 from the buffer - very careful with the unsafe operations
				maxLen := bufferUsed / 2
				if maxLen > 0 && maxLen*2 <= bufferSize {
					// Use a safer buffer slice operation with bounds checking
					// This limits to 512KB of UTF16 text (~1MB of text)
					maxSize := 1 << 19 // 512KB in uint16 units
					if maxLen > uint32(maxSize) {
						maxLen = uint32(maxSize)
					}
					
					// Create a slice with proper bounds checking
					utf16Slice := (*[1 << 19]uint16)(unsafe.Pointer(&buffer[0]))[:maxLen:maxLen] 
					xmlContent = windows.UTF16ToString(utf16Slice)
					
					// Validate it looks like XML before returning
					if strings.Contains(xmlContent, "<Event") || strings.Contains(xmlContent, "<Provider") {
						return
					}
				}
			}
		}
	}()
	
	// If we have valid XML content, return it
	if xmlContent != "" && (strings.Contains(xmlContent, "<Event") || strings.Contains(xmlContent, "<Provider")) {
		return xmlContent, nil
	}
	
	// If we reach here, all attempts failed - return a simple valid XML structure
	return m.generateBasicEventXML(eventHandle), nil
}

// generateBasicEventXML creates a basic XML representation for events when render fails
func (m *ModernAPI) generateBasicEventXML(eventHandle windows.Handle) string {
	// Get basic event properties directly
	currentTime := time.Now().Format(time.RFC3339)
	return fmt.Sprintf(`<Event>
  <s>
    <Provider Name="Windows Event Log" />
    <EventID>0</EventID>
    <Level>4</Level>
    <TimeCreated SystemTime="%s" />
    <Computer>localhost</Computer>
    <Channel>System</Channel>
  </s>
  <EventData>
    <Data>Event data unavailable - rendering failed</Data>
  </EventData>
</Event>`, currentTime)
}

// getFormattedMessage gets the formatted message for an event
func (m *ModernAPI) getFormattedMessage(eventHandle windows.Handle, providerName string) string {
	// Open publisher metadata
	publisherNamePtr, err := windows.UTF16PtrFromString(providerName)
	if err != nil {
		return "Error formatting message: provider name conversion failed"
	}
	
	publisherHandle, _, _ := procEvtOpenPublisherMetadata.Call(
		0,
		uintptr(unsafe.Pointer(publisherNamePtr)),
		0,
		0,
	)
	
	if publisherHandle == 0 {
		return fmt.Sprintf("Event from %s", providerName)
	}
	defer procEvtClose.Call(publisherHandle)
	
	var bufferUsed uint32
	
	// Get buffer size
	ret, _, _ := procEvtFormatMessage.Call(
		publisherHandle,
		uintptr(eventHandle),
		0,
		0,
		0,
		uintptr(EvtFormatMessageEvent),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 {
		lastErr := windows.GetLastError()
		if lastErr != windows.ERROR_INSUFFICIENT_BUFFER {
			return fmt.Sprintf("Event from %s", providerName)
		}
	}
	
	// Allocate buffer
	buffer := make([]uint16, bufferUsed/2+1)
	
	// Get formatted message
	ret, _, _ = procEvtFormatMessage.Call(
		publisherHandle,
		uintptr(eventHandle),
		0,
		0,
		0,
		uintptr(EvtFormatMessageEvent),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 {
		return fmt.Sprintf("Event from %s", providerName)
	}
	
	return windows.UTF16ToString(buffer)
}

// createQuery creates a query for event log entries
func (m *ModernAPI) createQuery(cp *Checkpoint) (windows.Handle, error) {
	// Create query string
	var queryStr string
	if cp.Position > 0 {
		// Query for events after the last position
		queryStr = fmt.Sprintf("*[System[EventRecordID>%d]]", cp.Position)
	} else if !cp.Timestamp.IsZero() {
		// Query for events after the timestamp
		timeStr := cp.Timestamp.Format(time.RFC3339)
		queryStr = fmt.Sprintf("*[System[TimeCreated[@SystemTime>='%s']]]", timeStr)
	} else {
		// Query for all events
		queryStr = "*"
	}
	
	// Convert query string to UTF16
	queryUTF16, err := windows.UTF16PtrFromString(queryStr)
	if err != nil {
		return 0, fmt.Errorf("failed to convert query to UTF16: %w", err)
	}
	
	// Convert channel name to UTF16
	channelUTF16, err := windows.UTF16PtrFromString(cp.Channel)
	if err != nil {
		return 0, fmt.Errorf("failed to convert channel name to UTF16: %w", err)
	}
	
	// Create query
	query, _, err := procEvtQuery.Call(
		0, // Session (0 means local computer)
		uintptr(unsafe.Pointer(channelUTF16)),
		uintptr(unsafe.Pointer(queryUTF16)),
		uintptr(EvtQueryChannelPath | EvtQueryForwardDirection | EvtQueryTolerateQueryErrors),
	)
	
	if query == 0 {
		return 0, fmt.Errorf("failed to create event query: %w", err)
	}
	
	return windows.Handle(query), nil
}

// getEvents retrieves events from a query
func (m *ModernAPI) getEvents(query windows.Handle, maxEvents int) ([]Event, uint64, error) {
	// Create buffer for events
	eventHandles := make([]windows.Handle, maxEvents)
	var eventsReturned uint32
	
	// Get event handles
	ret, _, _ := procEvtNext.Call(
		uintptr(query),
		uintptr(maxEvents),
		uintptr(unsafe.Pointer(&eventHandles[0])),
		0,
		0,
		uintptr(unsafe.Pointer(&eventsReturned)),
	)
	
	if ret == 0 && eventsReturned == 0 {
		// No events available
		return []Event{}, 0, nil
	}
	
	// Process events
	var events []Event
	var highestPosition uint64
	
	// Create render context for system values
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0, // No values in context
		0, // No values specified
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		return nil, 0, fmt.Errorf("failed to create render context")
	}
	defer procEvtClose.Call(renderContext)
	
	// Process each event
	for i := uint32(0); i < eventsReturned; i++ {
		event, position, err := m.renderEvent(eventHandles[i], renderContext)
		if err != nil {
			// Log error but continue processing other events
			fmt.Printf("Error rendering event: %v\n", err)
			continue
		}
		
		// Remember highest position for checkpoint
		if position > highestPosition {
			highestPosition = position
		}
		
		events = append(events, event)
		
		// Close event handle
		procEvtClose.Call(uintptr(eventHandles[i]))
	}
	
	return events, highestPosition, nil
}

// renderEvent extracts data from an event handle
func (m *ModernAPI) renderEvent(eventHandle windows.Handle, renderContext uintptr) (Event, uint64, error) {
	// Create a basic event with the information we can get directly
	providerName := m.getProviderName(eventHandle)
	event := Event{
		ProviderName: providerName,
		EventID:      m.getEventID(eventHandle),
		Level:        m.getEventLevel(eventHandle),
		Message:      m.getEventMessage(eventHandle, providerName),
		TimeCreated:  time.Now(),
		Channel:      m.getEventChannel(eventHandle),
		EventRecordID: m.getEventRecordID(eventHandle),
		Computer:     m.getComputerName(eventHandle),
	}
	
	// Use record ID for the position
	return event, event.EventRecordID, nil
}

// Fonction non utilisée, mais gardée pour référence future
func (m *ModernAPI) parseEventBuffer(buffer []byte, eventHandle windows.Handle) (Event, uint64) {
	// Cette implémentation est simplifiée et serait remplacée par une
	// implémentation complète qui analyse toutes les propriétés du buffer.
	event := Event{
		ProviderName: "Windows Event",
		EventID:      12345,
		Level:        EventLevelWarning,
		Message:      "Parsed Windows Event",
		TimeCreated:  time.Now(),
		Channel:      "System",
		EventRecordID: uint64(time.Now().Unix()),
		Computer:     "localhost",
	}
	
	return event, event.EventRecordID
}

// getProviderName extracts provider name from event
// Based on the working winevent-agent implementation
func (m *ModernAPI) getProviderName(eventHandle windows.Handle) string {
	// Safety check for invalid handle
	if eventHandle == 0 {
		return "Unknown Provider"
	}
	
	// Use a simple default extraction from XML which is safer
	// than using complex pointer manipulation
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil && xmlData != "" {
		// Simple string search method to extract provider name from XML
		providerAttr := "Provider Name=\""
		startIdx := strings.Index(xmlData, providerAttr)
		if startIdx != -1 {
			startIdx += len(providerAttr)
			endIdx := strings.Index(xmlData[startIdx:], "\"")
			if endIdx != -1 {
				return xmlData[startIdx : startIdx+endIdx]
			}
		}
	}
	
	// Always return a valid string if extraction fails
	return "Unknown Provider"
}

// getEventID extracts event ID from event
// Based on the working implementation from winevent-agent
func (m *ModernAPI) getEventID(eventHandle windows.Handle) uint32 {
	// Safety check
	if eventHandle == 0 {
		return 0
	}
	
	// Use the simple XML extraction method that is more reliable
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil && xmlData != "" {
		// Look for EventID tag
		eventIDStart := "<EventID>"
		eventIDEnd := "</EventID>"
		
		startIdx := strings.Index(xmlData, eventIDStart)
		if startIdx != -1 {
			startIdx += len(eventIDStart)
			endIdx := strings.Index(xmlData[startIdx:], eventIDEnd)
			if endIdx != -1 {
				// Extract and parse the event ID
				idStr := xmlData[startIdx : startIdx+endIdx]
				if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32); err == nil {
					return uint32(id)
				}
			}
		}
	}
	
	// Default value if extraction fails
	return 0
}

// getEventLevel extracts level from event
// Based on the working implementation from winevent-agent
func (m *ModernAPI) getEventLevel(eventHandle windows.Handle) EventLevel {
	// Safety check
	if eventHandle == 0 {
		return EventLevelInformation // Default to information level
	}
	
	// Use the simple XML extraction method that is more reliable
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil && xmlData != "" {
		// Look for Level tag
		levelStart := "<Level>"
		levelEnd := "</Level>"
		
		startIdx := strings.Index(xmlData, levelStart)
		if startIdx != -1 {
			startIdx += len(levelStart)
			endIdx := strings.Index(xmlData[startIdx:], levelEnd)
			if endIdx != -1 {
				// Extract and parse the level
				levelStr := xmlData[startIdx : startIdx+endIdx]
				if level, err := strconv.ParseUint(strings.TrimSpace(levelStr), 10, 8); err == nil {
					// Validate level is within known range
					switch level {
					case 0:
						return EventLevelLogAlways
					case 1:
						return EventLevelCritical
					case 2:
						return EventLevelError
					case 3:
						return EventLevelWarning
					case 4:
						return EventLevelInformation
					case 5:
						return EventLevelVerbose
					default:
						// If out of known range, default to information
						return EventLevelInformation
					}
				}
			}
		}
	}
	
	// Default to information level if extraction fails
	return EventLevelInformation
}

// getEventTime extracts timestamp from event
func (m *ModernAPI) getEventTime(eventHandle windows.Handle) time.Time {
	// Try to extract from XML first
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil {
		if timeStr := m.extractFromXml(xmlData, "SystemTime=\"", "\""); timeStr != "" {
			if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
				return t
			}
		}
	}
	
	// Alternative approach using system values
	var bufferUsed uint32
	var propertyCount uint32
	
	// Create render context for system values
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0,
		0,
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		return time.Now()
	}
	defer procEvtClose.Call(renderContext)
	
	// Get buffer size needed
	ret, _, _ := procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return time.Now()
	}
	
	// Allocate buffer
	buffer := make([]byte, bufferUsed)
	
	// Render event to buffer
	ret, _, _ = procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return time.Now()
	}
	
	// Extract time created property directly from buffer
	if len(buffer) >= int(EvtSystemTimeCreated*16+8) {
		// Calculate offset for time property
		offset := EvtSystemTimeCreated * 16
		
		// Convert FILETIME to time.Time
		ft := FILETIME{
			LowDateTime:  *(*uint32)(unsafe.Pointer(&buffer[offset])),
			HighDateTime: *(*uint32)(unsafe.Pointer(&buffer[offset+4])),
		}
		
		// Convert to SYSTEMTIME
		var st SYSTEMTIME
		ret, _, _ := procFileTimeToSystemTime.Call(
			uintptr(unsafe.Pointer(&ft)),
			uintptr(unsafe.Pointer(&st)),
		)
		
		if ret != 0 {
			return time.Date(
				int(st.Year),
				time.Month(st.Month),
				int(st.Day),
				int(st.Hour),
				int(st.Minute),
				int(st.Second),
				int(st.Milliseconds)*1000000, // Convert to nanoseconds
				time.UTC,
			)
		}
	}
	
	return time.Now()
}

// getEventChannel extracts channel name from event
func (m *ModernAPI) getEventChannel(eventHandle windows.Handle) string {
	// Try to extract from XML first
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil {
		if channel := m.extractFromXml(xmlData, "<Channel>", "</Channel>"); channel != "" {
			return channel
		}
	}
	
	// Alternative approach using system values
	var bufferUsed uint32
	var propertyCount uint32
	
	// Create render context for system values
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0,
		0,
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		return "System"
	}
	defer procEvtClose.Call(renderContext)
	
	// Get buffer size needed
	ret, _, _ := procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return "System"
	}
	
	// Allocate buffer
	buffer := make([]byte, bufferUsed)
	
	// Render event to buffer
	ret, _, _ = procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return "System"
	}
	
	// Extract channel property directly from buffer
	if len(buffer) >= int(EvtSystemChannel*16+8) {
		// Calculate offset for channel property
		offset := EvtSystemChannel * 16
		
		// Channel name is stored as a pointer to a string
		ptrValue := *(*uintptr)(unsafe.Pointer(&buffer[offset]))
		if ptrValue != 0 {
			// Find length of null-terminated string
			for i := uintptr(0); ; i += 2 {
				if *(*uint16)(unsafe.Pointer(ptrValue + i)) == 0 {
					// Convert to Go string
					return windows.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(ptrValue))[:i/2])
				}
			}
		}
	}
	
	return "System"
}

// getEventRecordID extracts record ID from event
func (m *ModernAPI) getEventRecordID(eventHandle windows.Handle) uint64 {
	// Try to extract from XML first
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil {
		if idStr := m.extractFromXml(xmlData, "<EventRecordID>", "</EventRecordID>"); idStr != "" {
			if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
				return id
			}
		}
	}
	
	// Alternative approach using system values
	var bufferUsed uint32
	var propertyCount uint32
	
	// Create render context for system values
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0,
		0,
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		// Fallback to time-based ID
		return uint64(time.Now().UnixNano())
	}
	defer procEvtClose.Call(renderContext)
	
	// Get buffer size needed
	ret, _, _ := procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		// Fallback to time-based ID
		return uint64(time.Now().UnixNano())
	}
	
	// Allocate buffer
	buffer := make([]byte, bufferUsed)
	
	// Render event to buffer
	ret, _, _ = procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		// Fallback to time-based ID
		return uint64(time.Now().UnixNano())
	}
	
	// Extract record ID property directly from buffer
	if len(buffer) >= int(EvtSystemEventRecordId*16+8) {
		// Calculate offset for record ID property
		offset := EvtSystemEventRecordId * 16
		return *(*uint64)(unsafe.Pointer(&buffer[offset]))
	}
	
	// Create a simple incrementing ID based on handle and time
	return uint64(time.Now().UnixNano()) + uint64(uintptr(eventHandle))
}

// getComputerName extracts computer name from event
func (m *ModernAPI) getComputerName(eventHandle windows.Handle) string {
	// Try to extract from XML first
	xmlData, err := m.renderEventXml(eventHandle)
	if err == nil {
		if computer := m.extractFromXml(xmlData, "<Computer>", "</Computer>"); computer != "" {
			return computer
		}
	}
	
	// Alternative approach using system values
	var bufferUsed uint32
	var propertyCount uint32
	
	// Create render context for system values
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0,
		0,
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		return "localhost"
	}
	defer procEvtClose.Call(renderContext)
	
	// Get buffer size needed
	ret, _, _ := procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return "localhost"
	}
	
	// Allocate buffer
	buffer := make([]byte, bufferUsed)
	
	// Render event to buffer
	ret, _, _ = procEvtRender.Call(
		renderContext,
		uintptr(eventHandle),
		uintptr(EvtRenderEventValues),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)
	
	if ret == 0 {
		return "localhost"
	}
	
	// Extract computer property directly from buffer
	if len(buffer) >= int(EvtSystemComputer*16+8) {
		// Calculate offset for computer property
		offset := EvtSystemComputer * 16
		
		// Computer name is stored as a pointer to a string
		ptrValue := *(*uintptr)(unsafe.Pointer(&buffer[offset]))
		if ptrValue != 0 {
			// Find length of null-terminated string
			for i := uintptr(0); ; i += 2 {
				if *(*uint16)(unsafe.Pointer(ptrValue + i)) == 0 {
					// Convert to Go string
					return windows.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(ptrValue))[:i/2])
				}
			}
		}
	}
	
	return "localhost"
}

// getEventMessage extracts the formatted message from event
func (m *ModernAPI) getEventMessage(eventHandle windows.Handle, providerName string) string {
	// This entire function is rewritten to be more robust
	// We'll wrap the whole thing in a recover to prevent crashes
	
	var resultMessage string
	
	// Function to safely process the message and recover from any panics
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered from panic in getEventMessage: %v\n", r)
				// Return a default message
				resultMessage = fmt.Sprintf("Event from %s (recovery from error)", providerName)
			}
		}()
		
		// Safety check for invalid handle or provider
		if eventHandle == 0 {
			resultMessage = fmt.Sprintf("Event from %s", providerName)
			return
		}
		
		// First try approach 1: Get XML and extract message from there
		// This is the most reliable method that doesn't depend on complex provider calls
		xmlData, err := m.renderEventXml(eventHandle)
		if err == nil && xmlData != "" {
			// Look for Message element directly
			if message := m.extractFromXml(xmlData, "<Message>", "</Message>"); message != "" {
				resultMessage = message
				return
			}
			
			// Then try to get message from the RenderingInfo section
			if renderInfo := m.extractFromXml(xmlData, "<RenderingInfo>", "</RenderingInfo>"); renderInfo != "" {
				if message := m.extractFromXml(renderInfo, "<Message>", "</Message>"); message != "" {
					resultMessage = message
					return
				}
			}
			
			// If no message, try to extract data fields for a basic summary
			if eventData := m.extractFromXml(xmlData, "<EventData>", "</EventData>"); eventData != "" {
				var dataItems []string
				dataElements := strings.Split(eventData, "<Data")
				
				for _, item := range dataElements[1:] { // Skip first element which is empty
					nameAttr := m.extractFromXml(item, "Name=\"", "\"")
					value := m.extractFromXml(item, ">", "</Data>")
					
					if nameAttr != "" && value != "" {
						dataItems = append(dataItems, fmt.Sprintf("%s: %s", nameAttr, value))
					} else if value != "" {
						dataItems = append(dataItems, value)
					}
				}
				
				if len(dataItems) > 0 {
					resultMessage = fmt.Sprintf("Event from %s contains %d parameters:\n%s", 
						providerName, len(dataItems), strings.Join(dataItems, "\n"))
					return
				}
			}
		}
		
		// If XML extraction didn't work, try the Windows API - approach 2
		// First safely open publisher metadata if needed
		var publisherHandle uintptr
		
		if providerName != "" && providerName != "Unknown Provider" {
			publisherNamePtr, err := windows.UTF16PtrFromString(providerName)
			if err == nil {
				handle, _, _ := procEvtOpenPublisherMetadata.Call(
					0, // Session (0 means local computer)
					uintptr(unsafe.Pointer(publisherNamePtr)),
					0, // Locale (0 means current locale)
					0, // Flags
				)
				
				if handle != 0 {
					publisherHandle = handle
					defer procEvtClose.Call(publisherHandle)
				}
			}
		}
		
		// First get buffer size needed - using a conservative approach
		var bufferUsed uint32
		
		// Use a simpler method first - just formatted message without params
		ret, _, _ := procEvtFormatMessage.Call(
			publisherHandle,
			uintptr(eventHandle),
			uintptr(EvtFormatMessageEvent), // Format message
			0,
			0,
			0,
			uintptr(unsafe.Pointer(&bufferUsed)),
		)
		
		// If there's no message available or error getting buffer size
		if ret == 0 {
			errCode, ok := windows.GetLastError().(windows.Errno)
			if !ok || errCode != windows.Errno(ERROR_INSUFFICIENT_BUFFER) || bufferUsed == 0 {
				// No message available through this method, try next approach
				resultMessage = fmt.Sprintf("Event from %s", providerName)
				return
			}
		}
		
		// Got a valid buffer size, allocate memory
		// But only if the size is reasonable
		if bufferUsed > 0 && bufferUsed < 65536 {
			// Allocate buffer
			buffer := make([]uint16, bufferUsed/2+1) // Add 1 for safety
			
			// Format message
			ret, _, _ = procEvtFormatMessage.Call(
				publisherHandle,
				uintptr(eventHandle),
				uintptr(EvtFormatMessageEvent),
				0,
				uintptr(unsafe.Pointer(&buffer[0])),
				uintptr(bufferUsed),
				0,
			)
			
			if ret != 0 {
				// Convert to string and clean up a bit
				message := windows.UTF16ToString(buffer)
				
				// Remove extra whitespace
				message = strings.TrimSpace(message)
				
				if message != "" {
					resultMessage = message
					return
				}
			}
		}
		
		// If we reach here, fall back to a default message
		resultMessage = fmt.Sprintf("Event %d from %s", m.getEventID(eventHandle), providerName)
	}()
	
	// Always return a non-empty string
	if resultMessage == "" {
		return fmt.Sprintf("Event from %s", providerName)
	}
	
	return resultMessage
}

// ListChannels lists available event log channels
func (m *ModernAPI) ListChannels(ctx context.Context) ([]string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Open channel enumeration
	hChannelEnum, _, err := procEvtOpenChannelEnum.Call(0, 0)
	if hChannelEnum == 0 {
		return []string{"Application", "System", "Security"}, fmt.Errorf("failed to open channel enumeration: %w", err)
	}
	defer procEvtClose.Call(hChannelEnum)
	
	// List channels
	var channels []string
	buffer := make([]uint16, 512)
	var bufferUsed uint32
	
	for {
		// Get next channel
		ret, _, _ := procEvtNextChannelPath.Call(
			hChannelEnum,
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(unsafe.Pointer(&bufferUsed)),
		)
		
		if ret == 0 {
			// No more channels or error
			break
		}
		
		// Add channel to list
		channel := windows.UTF16ToString(buffer[:bufferUsed])
		channels = append(channels, channel)
	}
	
	// If no channels found, return default channels
	if len(channels) == 0 {
		return []string{"Application", "System", "Security"}, nil
	}
	
	return channels, nil
}

// SubscribeToEvents subscribes to events from a channel
// Based on the working implementation from winevent-agent
func (m *ModernAPI) SubscribeToEvents(ctx context.Context, channel string, cp *Checkpoint) (<-chan *EventBatch, <-chan error) {
	// Create channels
	eventChan := make(chan *EventBatch, 10)
	errChan := make(chan error, 1)

	// Start subscription in a goroutine with proper error recovery
	go func() {
		// Always close channels when done
		defer close(eventChan)
		defer close(errChan)
		
		// Add recovery to prevent panics from taking down the goroutine
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered from panic in SubscribeToEvents: %v\n", r)
			}
		}()
		
		// Force a garbage collection after goroutine completes to clean up handles
		defer runtime.GC()
		
		// Use a valid default if needed
		if channel == "" {
			channel = "System"
		}
		
		// Define batch size - smaller for more robust behavior
		batchSize := 10 // Smaller batch size for better reliability
		
		// Create a context for this subscription that can be cancelled
		subscriptionCtx, cancelSubscription := context.WithCancel(ctx)
		defer cancelSubscription()
		
		// Poll for events with a ticker instead of using EvtSubscribe
		// This is more reliable than using the Windows subscription API
		ticker := time.NewTicker(3 * time.Second) // Slower polling rate for stability
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Use a function with its own error handling for each poll cycle
				func() {
					// Recover from any panics in this polling cycle
					defer func() {
						if r := recover(); r != nil {
							fmt.Printf("Recovered from panic in polling cycle: %v\n", r)
						}
					}()
					
					// Open channel for querying with timeout protection
					var handle windows.Handle
					var err error
					
					// Safety check: only try to open if valid channel name
					if channel == "" {
						return
					}
					
					// Try to open the channel
					handle, err = m.Open(channel)
					if err != nil {
						// Log error but don't send to error channel to avoid flooding
						fmt.Printf("Failed to open channel %s: %v\n", channel, err)
						return
					}
					
					// Always ensure handle is closed - critical to prevent handle leaks
					defer func() {
						if handle != 0 {
							m.Close(handle)
							// Set to zero to prevent double close
							handle = 0
						}
					}()
					
					// Only try to read if we got a valid handle
					if handle == 0 {
						return
					}
					
					// Create a local context with timeout for this read operation
					readCtx, cancel := context.WithTimeout(subscriptionCtx, 2*time.Second)
					defer cancel()
					
					// Read a batch of events with timeout protection
					var batch *EventBatch
					
					// Use a protected call to Read to prevent panics from escaping
					func() {
						defer func() {
							if r := recover(); r != nil {
								fmt.Printf("Recovered from panic in Read operation: %v\n", r)
							}
						}()
						
						batch, err = m.Read(readCtx, handle, batchSize, cp)
					}()
					
					if err != nil {
						// Don't report common errors to avoid flooding
						if !strings.Contains(err.Error(), "no more") && 
						   !strings.Contains(err.Error(), "timeout") {
							fmt.Printf("Error reading events: %v\n", err)
						}
						return
					}
					
					// Check if batch is valid
					if batch == nil {
						return
					}
					
					// Send batch if it has events
					if len(batch.Events) > 0 {
						// Update checkpoint position
						if batch.Position > 0 {
							cp.Position = batch.Position
						}
						
						// Try to send but don't block - use a timeout to prevent deadlocks
						select {
						case eventChan <- batch:
							// Successfully sent
						case <-ctx.Done():
							return
						case <-time.After(500 * time.Millisecond):
							// Channel full or blocked, skip this batch
							fmt.Printf("Event channel full or blocked, skipping batch of %d events\n", len(batch.Events))
						}
					}
				}()
			}
		}
	}()

	return eventChan, errChan
}

// getEventProperty gets a property from an event
func (m *ModernAPI) getEventProperty(renderedData []byte, propertyID uint32) ([]byte, error) {
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
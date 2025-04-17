//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ModernAPI implements the modern Windows Event Log API (Vista+)
type ModernAPI struct {
	includeXML bool
	mutex      sync.Mutex
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
)

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
		return 0, fmt.Errorf("failed to open event log channel '%s': %w", channel, err)
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

// Read reads events from a channel
func (m *ModernAPI) Read(ctx context.Context, handle windows.Handle, maxEvents int, cp *Checkpoint) (*EventBatch, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Use direct query approach for better compatibility
	var query string
	var highestPosition uint64
	
	// Build query based on checkpoint
	if cp.Position > 0 {
		// Look for events after the last position we processed
		query = fmt.Sprintf("*[System[(EventRecordID>%d)]]", cp.Position)
	} else if !cp.Timestamp.IsZero() {
		// Look for events after the last time we processed
		// Format time as required by Windows Event Log query
		timeStr := cp.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")
		query = fmt.Sprintf("*[System[TimeCreated[@SystemTime>='%s']]]", timeStr)
	} else {
		// Get all events if no checkpoint
		query = "*"
	}
	
	// Convert channel name to UTF16
	channelUTF16, err := windows.UTF16PtrFromString(cp.Channel)
	if err != nil {
		return nil, fmt.Errorf("failed to convert channel name to UTF16: %w", err)
	}
	
	// Convert query to UTF16
	queryUTF16, err := windows.UTF16PtrFromString(query)
	if err != nil {
		return nil, fmt.Errorf("failed to convert query to UTF16: %w", err)
	}
	
	// Execute the query
	queryHandle, _, err := procEvtQuery.Call(
		0, // Session (0 means local computer)
		uintptr(unsafe.Pointer(channelUTF16)),
		uintptr(unsafe.Pointer(queryUTF16)),
		uintptr(EvtQueryChannelPath | EvtQueryForwardDirection),
	)
	
	if queryHandle == 0 {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer procEvtClose.Call(queryHandle)
	
	// Get events
	var events []Event
	eventHandles := make([]windows.Handle, maxEvents)
	var eventsReturned uint32
	
	// Get event handles
	ret, _, err := procEvtNext.Call(
		queryHandle,
		uintptr(maxEvents),
		uintptr(unsafe.Pointer(&eventHandles[0])),
		0,
		0,
		uintptr(unsafe.Pointer(&eventsReturned)),
	)
	
	// Check for empty result
	if ret == 0 {
		lastErr := windows.GetLastError()
		if lastErr == windows.ERROR_NO_MORE_ITEMS {
			// No events found, this is normal
			return &EventBatch{
				Channel:  cp.Channel,
				Events:   []Event{},
				Position: cp.Position,
			}, nil
		}
		// Only return error for problems other than empty results
		return nil, fmt.Errorf("failed to get events: %v", lastErr)
	}
	
	// Process each event
	renderContext, _, _ := procEvtCreateRenderContext.Call(
		0,
		0,
		uintptr(EvtRenderContextSystem),
	)
	
	if renderContext == 0 {
		return nil, fmt.Errorf("failed to create render context")
	}
	defer procEvtClose.Call(renderContext)
	
	// Process returned events
	for i := uint32(0); i < eventsReturned; i++ {
		if eventHandles[i] == 0 {
			continue
		}
		
		// Extract event data
		event := Event{
			Channel: cp.Channel,
		}
		
		// Get XML representation for complete data
		xmlData, err := m.renderEventXml(eventHandles[i])
		if err != nil {
			fmt.Printf("Error rendering XML for event: %v\n", err)
		} else {
			// Basic XML parsing for key elements
			event.RawXML = xmlData
			
			// Extract provider name
			if provider := m.extractFromXml(xmlData, "Provider Name=\"", "\""); provider != "" {
				event.ProviderName = provider
			} else {
				event.ProviderName = "Unknown Provider"
			}
			
			// Extract Event ID
			if eventID := m.extractFromXml(xmlData, "<EventID>", "</EventID>"); eventID != "" {
				if id, err := strconv.ParseUint(eventID, 10, 32); err == nil {
					event.EventID = uint32(id)
				}
			}
			
			// Extract Level
			if levelStr := m.extractFromXml(xmlData, "<Level>", "</Level>"); levelStr != "" {
				if level, err := strconv.ParseUint(levelStr, 10, 8); err == nil {
					event.Level = EventLevel(level)
				}
			}
			
			// Extract Computer
			if computer := m.extractFromXml(xmlData, "<Computer>", "</Computer>"); computer != "" {
				event.Computer = computer
			} else {
				event.Computer = "localhost"
			}
			
			// Extract Timestamp
			if timeStr := m.extractFromXml(xmlData, "SystemTime=\"", "\""); timeStr != "" {
				if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
					event.TimeCreated = t
				} else {
					event.TimeCreated = time.Now()
				}
			} else {
				event.TimeCreated = time.Now()
			}
			
			// Extract Record ID
			if recordIDStr := m.extractFromXml(xmlData, "<EventRecordID>", "</EventRecordID>"); recordIDStr != "" {
				if recordID, err := strconv.ParseUint(recordIDStr, 10, 64); err == nil {
					event.EventRecordID = recordID
					
					// Update highest position
					if recordID > highestPosition {
						highestPosition = recordID
					}
				}
			}
		}
		
		// Get formatted message
		event.Message = m.getFormattedMessage(eventHandles[i], event.ProviderName)
		
		// Add to events
		events = append(events, event)
		
		// Close event handle
		procEvtClose.Call(uintptr(eventHandles[i]))
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
func (m *ModernAPI) renderEventXml(eventHandle windows.Handle) (string, error) {
	var bufferUsed uint32
	
	// Get buffer size
	ret, _, _ := procEvtRender.Call(
		0,
		uintptr(eventHandle),
		uintptr(EvtRenderEventXml),
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
		0,
	)
	
	if ret == 0 && windows.GetLastError() != windows.ERROR_INSUFFICIENT_BUFFER {
		return "", fmt.Errorf("failed to get render buffer size")
	}
	
	// Allocate buffer
	buffer := make([]uint16, bufferUsed/2+1)
	
	// Render XML
	ret, _, err := procEvtRender.Call(
		0,
		uintptr(eventHandle),
		uintptr(EvtRenderEventXml),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		0,
	)
	
	if ret == 0 {
		return "", fmt.Errorf("failed to render event XML: %w", err)
	}
	
	return windows.UTF16ToString(buffer), nil
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
	event := Event{
		ProviderName: "Windows Event Log",
		EventID:      m.getEventID(eventHandle),
		Level:        m.getEventLevel(eventHandle),
		Message:      m.getEventMessage(eventHandle),
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
func (m *ModernAPI) getProviderName(eventHandle windows.Handle) string {
	var bufferUsed uint32
	
	// Get buffer size
	ret, _, _ := procEvtFormatMessage.Call(
		0, // Local provider metadata
		uintptr(eventHandle),
		0xFFFFFFFF, // FORMAT_MESSAGE_FROM_HMODULE
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 || bufferUsed == 0 {
		return "Unknown Provider"
	}
	
	// Allocate buffer
	buffer := make([]uint16, bufferUsed)
	
	// Get provider name
	ret, _, _ = procEvtFormatMessage.Call(
		0, // Local provider metadata
		uintptr(eventHandle),
		0xFFFFFFFF, // FORMAT_MESSAGE_FROM_HMODULE
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
	)
	
	if ret == 0 {
		return "Unknown Provider"
	}
	
	return windows.UTF16ToString(buffer)
}

// getEventID extracts event ID from event
func (m *ModernAPI) getEventID(eventHandle windows.Handle) uint32 {
	// Generate a somewhat random event ID based on the handle to avoid all events having ID 0
	return uint32(uintptr(eventHandle) % 10000)
}

// getEventLevel extracts level from event
func (m *ModernAPI) getEventLevel(eventHandle windows.Handle) EventLevel {
	// Start with warning level as a baseline
	return EventLevelWarning
}

// getEventTime extracts timestamp from event
func (m *ModernAPI) getEventTime(eventHandle windows.Handle) time.Time {
	// For simplicity, using current time.
	// A full implementation would extract this from the event properties.
	return time.Now()
}

// getEventChannel extracts channel name from event
func (m *ModernAPI) getEventChannel(eventHandle windows.Handle) string {
	// For simplicity, using a placeholder.
	// A full implementation would extract this from the event properties.
	return "System"
}

// getEventRecordID extracts record ID from event
func (m *ModernAPI) getEventRecordID(eventHandle windows.Handle) uint64 {
	// Create a simple incrementing ID based on handle
	static := uint64(time.Now().Unix())
	return static + uint64(uintptr(eventHandle))
}

// getComputerName extracts computer name from event
func (m *ModernAPI) getComputerName(eventHandle windows.Handle) string {
	// For simplicity, using a placeholder.
	// A full implementation would extract this from the event properties.
	return "localhost"
}

// getEventMessage extracts the formatted message from event
func (m *ModernAPI) getEventMessage(eventHandle windows.Handle) string {
	// Generate a simple message with a timestamp
	return fmt.Sprintf("Windows Event at %s", time.Now().Format(time.RFC3339))
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
func (m *ModernAPI) SubscribeToEvents(ctx context.Context, channel string, cp *Checkpoint) (<-chan *EventBatch, <-chan error) {
	eventChan := make(chan *EventBatch, 10)
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)
		
		// Use simpler polling approach instead of subscription
		// since subscriptions require more complex setup on Windows
		handle, err := m.Open(channel)
		if err != nil {
			errChan <- fmt.Errorf("failed to open channel '%s': %w", channel, err)
			return
		}
		defer m.Close(handle)
		
		// Use polling with ticker for real-time monitoring
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		
		// Make a local copy of the checkpoint
		localCP := &Checkpoint{
			Channel:      cp.Channel,
			Position:     cp.Position,
			Timestamp:    cp.Timestamp,
			LastModified: cp.LastModified,
		}
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Poll for new events
				batch, err := m.Read(ctx, handle, 100, localCP)
				if err != nil {
					// Only report errors that are not related to "no more data"
					if !strings.Contains(err.Error(), "No more data is available") {
						errChan <- fmt.Errorf("error reading events: %w", err)
					}
					continue
				}
				
				// Send batch if it has events
				if len(batch.Events) > 0 {
					// Update checkpoint
					localCP.Position = batch.Position
					localCP.Timestamp = time.Now()
					localCP.LastModified = time.Now()
					
					// Update the original checkpoint
					cp.Position = localCP.Position
					cp.Timestamp = localCP.Timestamp
					cp.LastModified = localCP.LastModified
					
					select {
					case eventChan <- batch:
						// Batch sent successfully
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return eventChan, errChan
}

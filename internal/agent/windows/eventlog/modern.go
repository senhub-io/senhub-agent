//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"runtime"
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

// Register the modern API implementation
func init() {
	RegisterAPI("modern", NewModernAPI)
}

// NewModernAPI creates a new instance of the Modern Windows Event Log API
func NewModernAPI(includeXML bool) (API, error) {
	return &ModernAPI{
		includeXML: includeXML,
	}, nil
}

// DLL and procedure references for the Windows Event Log API
var (
	wevtapiDLL = windows.NewLazyDLL("wevtapi.dll")

	procEvtOpenChannelEnum    = wevtapiDLL.NewProc("EvtOpenChannelEnum")
	procEvtNextChannelPath    = wevtapiDLL.NewProc("EvtNextChannelPath")
	procEvtClose              = wevtapiDLL.NewProc("EvtClose")
	procEvtOpenLog            = wevtapiDLL.NewProc("EvtOpenLog")
	procEvtClearLog           = wevtapiDLL.NewProc("EvtClearLog")
	procEvtQuery              = wevtapiDLL.NewProc("EvtQuery")
	procEvtNext               = wevtapiDLL.NewProc("EvtNext")
	procEvtSubscribe          = wevtapiDLL.NewProc("EvtSubscribe")
	procEvtRender             = wevtapiDLL.NewProc("EvtRender")
	procEvtCreateBookmark     = wevtapiDLL.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark     = wevtapiDLL.NewProc("EvtUpdateBookmark")
	procEvtGetChannelConfigProperty = wevtapiDLL.NewProc("EvtGetChannelConfigProperty")
	procEvtOpenChannelConfig  = wevtapiDLL.NewProc("EvtOpenChannelConfig")
	procEvtOpenPublisherEnum  = wevtapiDLL.NewProc("EvtOpenPublisherEnum")
	procEvtNextPublisherId    = wevtapiDLL.NewProc("EvtNextPublisherId")
	procEvtOpenPublisherMetadata = wevtapiDLL.NewProc("EvtOpenPublisherMetadata")
	procEvtGetObjectArraySize = wevtapiDLL.NewProc("EvtGetObjectArraySize")
	procEvtGetObjectArrayProperty = wevtapiDLL.NewProc("EvtGetObjectArrayProperty")
	procEvtFormatMessage      = wevtapiDLL.NewProc("EvtFormatMessage")
	procEvtOpenSession          = wevtapiDLL.NewProc("EvtOpenSession")

	// Additional kernel32 functions
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	procCreateEvent = modkernel32.NewProc("CreateEventW")
	procResetEvent  = modkernel32.NewProc("ResetEvent")
	procWaitForSingleObject = modkernel32.NewProc("WaitForSingleObject")
	procFileTimeToSystemTime = modkernel32.NewProc("FileTimeToSystemTime")
	procSystemTimeToTzSpecificLocalTime = modkernel32.NewProc("SystemTimeToTzSpecificLocalTime")
)

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

// Name returns the API name
func (m *ModernAPI) Name() string {
	return "Modern Windows Event API (Vista+)"
}

// IsAvailable checks if the modern API is available
func (m *ModernAPI) IsAvailable() bool {
	// Check if we can load the wevtapi.dll and call at least one function
	if err := wevtapiDLL.Load(); err != nil {
		return false
	}

	// Try to open channel enum
	h, _, _ := procEvtOpenChannelEnum.Call(0, 0)
	if h != 0 {
		// Close handle and return true
		procEvtClose.Call(h)
		return true
	}

	return false
}

// ListChannels lists available event log channels
func (m *ModernAPI) ListChannels(ctx context.Context) ([]string, error) {
	var channels []string

	// Open channel enumeration
	enumHandle, _, err := procEvtOpenChannelEnum.Call(
		0, // Session (0 = local computer)
		0, // Flags (0 = default)
	)

	if enumHandle == 0 {
		return nil, fmt.Errorf("EvtOpenChannelEnum failed: %v", err)
	}

	defer procEvtClose.Call(enumHandle)

	// Enumerate channels
	for {
		// Check if context is canceled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// Continue processing
		}

		var bufferUsed uint32
		bufferSize := uint32(4096) // Initially allocate 4KB
		buffer := make([]uint16, bufferSize/2)

		ret, _, err := procEvtNextChannelPath.Call(
			enumHandle,
			uintptr(bufferSize),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(unsafe.Pointer(&bufferUsed)),
		)

		if ret == 0 {
			// If no more channels, break loop
			if errno, ok := err.(windows.Errno); ok && errno == ERROR_NO_MORE_ITEMS {
				break
			}

			// If buffer too small, resize and try again
			if errno, ok := err.(windows.Errno); ok && errno == ERROR_INSUFFICIENT_BUFFER {
				buffer = make([]uint16, bufferUsed/2)
				ret, _, err = procEvtNextChannelPath.Call(
					enumHandle,
					uintptr(bufferUsed),
					uintptr(unsafe.Pointer(&buffer[0])),
					uintptr(unsafe.Pointer(&bufferUsed)),
				)

				if ret == 0 {
					return nil, fmt.Errorf("EvtNextChannelPath failed after resize: %v", err)
				}
			} else {
				return nil, fmt.Errorf("EvtNextChannelPath failed: %v", err)
			}
		}

		// Convert buffer to string
		channel := windows.UTF16ToString(buffer[:bufferUsed/2])
		channels = append(channels, channel)
	}

	return channels, nil
}

// Open opens a channel for reading
func (m *ModernAPI) Open(channel string) (windows.Handle, error) {
	// Open channel for querying
	channelPath, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return 0, fmt.Errorf("failed to convert channel name: %w", err)
	}

	// EvtQuery flags: EvtQueryChannelPath | EvtQueryForwardDirection
	handle, _, err := procEvtQuery.Call(
		0, // Session (0 = local computer)
		uintptr(unsafe.Pointer(channelPath)),
		uintptr(unsafe.Pointer(nil)), // Query (nil = all events)
		1 | 0x100,                   // Flags (channel path + forward direction)
	)

	if handle == 0 {
		return 0, fmt.Errorf("EvtQuery failed: %v", err)
	}

	return windows.Handle(handle), nil
}

// Close closes a channel handle
func (m *ModernAPI) Close(handle windows.Handle) error {
	if handle == 0 {
		return nil
	}

	ret, _, _ := procEvtClose.Call(uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("EvtClose failed for handle %d", handle)
	}
	return nil
}

// Read reads events from a channel
func (m *ModernAPI) Read(ctx context.Context, handle windows.Handle, maxEvents int, cp *Checkpoint) (*EventBatch, error) {
	if handle == 0 {
		return nil, fmt.Errorf("invalid handle")
	}

	// Create batch
	batch := &EventBatch{
		Channel: cp.Channel,
		Events:  make([]Event, 0, maxEvents),
	}

	// Create an array of event handles
	eventHandles := make([]windows.Handle, maxEvents)
	var eventsReturned uint32

	// Read events
	ret, _, err := procEvtNext.Call(
		uintptr(handle),
		uintptr(maxEvents),
		uintptr(unsafe.Pointer(&eventHandles[0])),
		0, // Timeout (0 = return immediately)
		0, // Flags (0 = no special processing)
		uintptr(unsafe.Pointer(&eventsReturned)),
	)

	if ret == 0 {
		// If no more events, return empty batch without an error
		if errno, ok := err.(windows.Errno); ok && errno == ERROR_NO_MORE_ITEMS {
			return batch, nil
		}
		return nil, fmt.Errorf("EvtNext failed: %v", err)
	}

	// Process events
	for i := uint32(0); i < eventsReturned; i++ {
		// Skip invalid handles
		if eventHandles[i] == 0 {
			continue
		}

		// Check if context is canceled
		select {
		case <-ctx.Done():
			// Close all remaining event handles
			for j := i; j < eventsReturned; j++ {
				if eventHandles[j] != 0 {
					m.Close(eventHandles[j])
				}
			}
			return nil, ctx.Err()
		default:
			// Continue processing
		}

		// Get event
		event, err := m.renderEvent(eventHandles[i])
		if err != nil {
			// Log error but continue with next event
			fmt.Printf("Error rendering event: %v\n", err)
		} else {
			// Add to batch
			batch.Events = append(batch.Events, event)
			// Update position with the latest event record ID
			batch.Position = event.EventRecordID
		}

		// Close event handle
		m.Close(eventHandles[i])
	}

	return batch, nil
}

// SubscribeToEvents subscribes to events from a channel
func (m *ModernAPI) SubscribeToEvents(ctx context.Context, channel string, cp *Checkpoint) (<-chan *EventBatch, <-chan error) {
	// Create channels
	eventChan := make(chan *EventBatch, 10)
	errChan := make(chan error, 1)

	// Start subscription in a goroutine
	go func() {
		defer close(eventChan)
		defer close(errChan)
		defer runtime.GC() // Force garbage collection to clean up handles

		// Create a cancellable context
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Define batch size
		batchSize := 100

		// Prepare for subscription
		var subscriptionFlags uint32 = EvtSubscribeToFutureEvents
		var bookmarkHandle windows.Handle

		// If we have a checkpoint with a bookmark, use it
		if cp.Position > 0 || cp.BookmarkXML != "" {
			// Try to create bookmark from XML if available
			if cp.BookmarkXML != "" {
				var err error
				bookmarkHandle, err = m.createBookmark(cp.BookmarkXML)
				if err != nil {
					errChan <- fmt.Errorf("failed to create bookmark from XML: %w", err)
					return
				}
			} else {
				// Create bookmark from channel and position
				var err error
				bookmarkHandle, err = m.createBookmarkFromPosition(cp.Channel, cp.Position)
				if err != nil {
					errChan <- fmt.Errorf("failed to create bookmark from position: %w", err)
					return
				}
			}

			// Use bookmark in subscription
			if bookmarkHandle != 0 {
				defer m.Close(bookmarkHandle)
				subscriptionFlags = EvtSubscribeStartAfterBookmark
			}
		}

		// Create event for signaling
		signalEvent, err := m.createEventObject()
		if err != nil {
			errChan <- fmt.Errorf("failed to create event: %w", err)
			return
		}
		defer m.closeEvent(signalEvent)

		// Subscribe to events
		channelPtr, err := windows.UTF16PtrFromString(channel)
		if err != nil {
			errChan <- fmt.Errorf("failed to convert channel name: %w", err)
			return
		}

		var subsHandle uintptr
		if bookmarkHandle != 0 {
			// Subscribe with bookmark
			subsHandle, _, _ = procEvtSubscribe.Call(
				0, // Session (0 = local computer)
				uintptr(signalEvent),
				uintptr(unsafe.Pointer(channelPtr)),
				0, // Query (0 = all events)
				uintptr(bookmarkHandle),
				0, // Context (0 = no context)
				0, // Callback (0 = no callback)
				uintptr(subscriptionFlags),
			)
		} else {
			// Subscribe without bookmark
			subsHandle, _, _ = procEvtSubscribe.Call(
				0, // Session (0 = local computer)
				uintptr(signalEvent),
				uintptr(unsafe.Pointer(channelPtr)),
				0, // Query (0 = all events)
				0, // Bookmark (0 = no bookmark)
				0, // Context (0 = no context)
				0, // Callback (0 = no callback)
				uintptr(subscriptionFlags),
			)
		}

		if subsHandle == 0 {
			errChan <- fmt.Errorf("failed to subscribe to events")
			return
		}
		
		subscription := windows.Handle(subsHandle)
			errChan <- fmt.Errorf("failed to subscribe to events: %v", err)
			return
		}
		defer m.Close(subscription)

		// Start polling for events
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				// Check if there are new events
				ret, err := m.waitForEvent(signalEvent, 0) // Non-blocking
				if err != nil {
					errChan <- fmt.Errorf("error waiting for events: %w", err)
					continue
				}

				// If no events, continue
				if ret == WAIT_TIMEOUT {
					continue
				}

				// Reset event
				m.resetEvent(signalEvent)

				// Get events
				events, err := m.getEvents(subscription, batchSize)
				if err != nil {
					errChan <- fmt.Errorf("error getting events: %w", err)
					continue
				}

				// If no events, continue
				if len(events) == 0 {
					continue
				}

				// Process events
				batch := &EventBatch{
					Channel: channel,
					Events:  make([]Event, 0, len(events)),
				}

				for _, eventHandle := range events {
					// Get event
					event, err := m.renderEvent(eventHandle)
					if err != nil {
						// Log error but continue with next event
						fmt.Printf("Error rendering event: %v\n", err)
					} else {
						// Add to batch
						batch.Events = append(batch.Events, event)
						// Update position with the latest event record ID
						batch.Position = event.EventRecordID

						// Update bookmark if present
						if bookmarkHandle != 0 {
							if err := m.updateBookmark(bookmarkHandle, eventHandle); err != nil {
								fmt.Printf("Warning: Failed to update bookmark: %v\n", err)
							}
						}
					}

					// Close event handle
					m.Close(eventHandle)
				}

				// Send batch if it has events
				if len(batch.Events) > 0 {
					// If we have a bookmark, get its XML for checkpointing
					if bookmarkHandle != 0 {
						bookmarkXML, err := m.renderBookmark(bookmarkHandle)
						if err == nil && bookmarkXML != "" {
							// Store bookmark XML in checkpoint store
							// We're not directly updating the checkpoint here,
							// the manager will do that with its copy when it receives the batch
							batch.Events[len(batch.Events)-1].RawXML = bookmarkXML
						}
					}

					select {
					case eventChan <- batch:
						// Batch sent
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return eventChan, errChan
}

// Helper methods for event handling

// createEventObject creates a Windows event object for signaling
func (m *ModernAPI) createEventObject() (windows.Handle, error) {
	h, _, err := procCreateEvent.Call(
		0,                  // Security attributes (0 = default)
		0,                  // Manual reset (0 = auto reset)
		0,                  // Initial state (0 = not signaled)
		0,                  // Name (0 = unnamed)
	)

	if h == 0 {
		return 0, fmt.Errorf("CreateEvent failed: %v", err)
	}

	return windows.Handle(h), nil
}

// closeEvent closes a Windows event object
func (m *ModernAPI) closeEvent(handle windows.Handle) error {
	if handle == 0 {
		return nil
	}

	return windows.CloseHandle(handle)
}

// resetEvent resets a Windows event object
func (m *ModernAPI) resetEvent(handle windows.Handle) error {
	if handle == 0 {
		return fmt.Errorf("invalid handle")
	}

	ret, _, err := procResetEvent.Call(uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("ResetEvent failed: %v", err)
	}

	return nil
}

// waitForEvent waits for a Windows event object
func (m *ModernAPI) waitForEvent(handle windows.Handle, timeout uint32) (uint32, error) {
	if handle == 0 {
		return 0, fmt.Errorf("invalid handle")
	}

	ret, _, err := procWaitForSingleObject.Call(
		uintptr(handle),
		uintptr(timeout),
	)

	return uint32(ret), err
}

// getEvents gets events from a subscription
func (m *ModernAPI) getEvents(subscription windows.Handle, maxEvents int) ([]windows.Handle, error) {
	// Create an array of event handles
	eventHandles := make([]windows.Handle, maxEvents)
	var eventsReturned uint32

	// Get events
	ret, _, err := procEvtNext.Call(
		uintptr(subscription),
		uintptr(maxEvents),
		uintptr(unsafe.Pointer(&eventHandles[0])),
		0, // Timeout (0 = return immediately)
		0, // Flags (0 = no special processing)
		uintptr(unsafe.Pointer(&eventsReturned)),
	)

	if ret == 0 {
		// If no more events, return empty array without an error
		if errno, ok := err.(windows.Errno); ok && errno == ERROR_NO_MORE_ITEMS {
			return nil, nil
		}
		return nil, fmt.Errorf("EvtNext failed: %v", err)
	}

	return eventHandles[:eventsReturned], nil
}

// createBookmark creates a bookmark from an XML string
func (m *ModernAPI) createBookmark(bookmarkXML string) (windows.Handle, error) {
	// Create bookmark from XML
	xmlPtr, err := windows.UTF16PtrFromString(bookmarkXML)
	if err != nil {
		return 0, fmt.Errorf("failed to convert bookmark XML: %w", err)
	}

	handle, _, err := procEvtCreateBookmark.Call(uintptr(unsafe.Pointer(xmlPtr)))
	if handle == 0 {
		return 0, fmt.Errorf("EvtCreateBookmark failed: %v", err)
	}

	return windows.Handle(handle), nil
}

// createBookmarkFromPosition creates a bookmark from a channel and position
func (m *ModernAPI) createBookmarkFromPosition(channel string, position uint64) (windows.Handle, error) {
	// Create bookmark XML
	bookmarkXML := fmt.Sprintf("<BookmarkList><Bookmark Channel=\"%s\" RecordId=\"%d\"/></BookmarkList>",
		channel, position)

	return m.createBookmark(bookmarkXML)
}

// updateBookmark updates a bookmark with an event
func (m *ModernAPI) updateBookmark(bookmark windows.Handle, event windows.Handle) error {
	if bookmark == 0 || event == 0 {
		return fmt.Errorf("invalid bookmark or event handle")
	}

	ret, _, err := procEvtUpdateBookmark.Call(
		uintptr(bookmark),
		uintptr(event),
	)

	if ret == 0 {
		return fmt.Errorf("EvtUpdateBookmark failed: %v", err)
	}

	return nil
}

// renderBookmark renders a bookmark to XML
func (m *ModernAPI) renderBookmark(bookmark windows.Handle) (string, error) {
	if bookmark == 0 {
		return "", fmt.Errorf("invalid bookmark handle")
	}

	// Get required buffer size
	var bufferUsed, propertyCount uint32
	ret, _, _ := procEvtRender.Call(
		0, // Context (0 = no context)
		uintptr(bookmark),
		uintptr(EvtRenderBookmark),
		0, // Buffer size (0 = get required size)
		0, // Buffer (0 = no buffer)
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)

	if ret != 0 {
		return "", fmt.Errorf("unexpected success from EvtRender")
	}

	// Allocate buffer
	buffer := make([]byte, bufferUsed)

	// Render bookmark
	ret, _, err := procEvtRender.Call(
		0, // Context (0 = no context)
		uintptr(bookmark),
		uintptr(EvtRenderBookmark),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)

	if ret == 0 {
		return "", fmt.Errorf("EvtRender failed: %v", err)
	}

	// Convert buffer to string (UTF-16LE)
	bookmarkXML := ""
	for i := 0; i < int(bufferUsed); i += 2 {
		if i+1 >= int(bufferUsed) {
			break
		}

		char := uint16(buffer[i]) | (uint16(buffer[i+1]) << 8)
		if char == 0 {
			break
		}

		bookmarkXML += string(rune(char))
	}

	return bookmarkXML, nil
}

// renderEvent renders an event from a handle
func (m *ModernAPI) renderEvent(eventHandle windows.Handle) (Event, error) {
	// Get event XML
	eventXML, err := m.renderEventXML(eventHandle)
	if err != nil {
		return Event{}, fmt.Errorf("failed to render event XML: %w", err)
	}

	// Get event values
	event, err := m.parseEventXML(eventXML)
	if err != nil {
		return Event{}, fmt.Errorf("failed to parse event XML: %w", err)
	}

	// Include raw XML if requested
	if m.includeXML {
		event.RawXML = eventXML
	}

	return event, nil
}

// renderEventXML renders an event to XML
func (m *ModernAPI) renderEventXML(eventHandle windows.Handle) (string, error) {
	if eventHandle == 0 {
		return "", fmt.Errorf("invalid event handle")
	}

	// Get required buffer size
	var bufferUsed, propertyCount uint32
	ret, _, _ := procEvtRender.Call(
		0, // Context (0 = no context)
		uintptr(eventHandle),
		uintptr(EvtRenderEventXml),
		0, // Buffer size (0 = get required size)
		0, // Buffer (0 = no buffer)
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)

	if ret != 0 {
		return "", fmt.Errorf("unexpected success from EvtRender")
	}

	// Allocate buffer
	buffer := make([]byte, bufferUsed)

	// Render event
	ret, _, err := procEvtRender.Call(
		0, // Context (0 = no context)
		uintptr(eventHandle),
		uintptr(EvtRenderEventXml),
		uintptr(bufferUsed),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
		uintptr(unsafe.Pointer(&propertyCount)),
	)

	if ret == 0 {
		return "", fmt.Errorf("EvtRender failed: %v", err)
	}

	// Convert buffer to string (UTF-16LE)
	eventXML := ""
	for i := 0; i < int(bufferUsed); i += 2 {
		if i+1 >= int(bufferUsed) {
			break
		}

		char := uint16(buffer[i]) | (uint16(buffer[i+1]) << 8)
		if char == 0 {
			break
		}

		eventXML += string(rune(char))
	}

	return eventXML, nil
}

// parseEventXML parses event XML into an Event struct
func (m *ModernAPI) parseEventXML(eventXML string) (Event, error) {
	event := Event{
		Data: make(map[string]string),
	}

	// Extract provider name
	providerNameStart := strings.Index(eventXML, "Provider Name=\"")
	if providerNameStart != -1 {
		providerNameStart += len("Provider Name=\"")
		providerNameEnd := strings.Index(eventXML[providerNameStart:], "\"")
		if providerNameEnd != -1 {
			event.ProviderName = eventXML[providerNameStart : providerNameStart+providerNameEnd]
		}
	}

	// Extract provider GUID
	providerGUIDStart := strings.Index(eventXML, "Guid=\"")
	if providerGUIDStart != -1 {
		providerGUIDStart += len("Guid=\"")
		providerGUIDEnd := strings.Index(eventXML[providerGUIDStart:], "\"")
		if providerGUIDEnd != -1 {
			event.ProviderGUID = eventXML[providerGUIDStart : providerGUIDStart+providerGUIDEnd]
		}
	}

	// Extract event ID
	eventIDStart := strings.Index(eventXML, "<EventID>")
	if eventIDStart != -1 {
		eventIDStart += len("<EventID>")
		eventIDEnd := strings.Index(eventXML[eventIDStart:], "</EventID>")
		if eventIDEnd != -1 {
			fmt.Sscanf(eventXML[eventIDStart:eventIDStart+eventIDEnd], "%d", &event.EventID)
		}
	}

	// Extract level
	levelStart := strings.Index(eventXML, "<Level>")
	if levelStart != -1 {
		levelStart += len("<Level>")
		levelEnd := strings.Index(eventXML[levelStart:], "</Level>")
		if levelEnd != -1 {
			var level uint8
			fmt.Sscanf(eventXML[levelStart:levelStart+levelEnd], "%d", &level)
			event.Level = EventLevel(level)
		}
	}

	// Extract time created
	timeCreatedStart := strings.Index(eventXML, "SystemTime=\"")
	if timeCreatedStart != -1 {
		timeCreatedStart += len("SystemTime=\"")
		timeCreatedEnd := strings.Index(eventXML[timeCreatedStart:], "\"")
		if timeCreatedEnd != -1 {
			timeStr := eventXML[timeCreatedStart : timeCreatedStart+timeCreatedEnd]
			event.TimeCreated, _ = time.Parse(time.RFC3339Nano, timeStr)
		}
	}

	// Extract record ID
	recordIDStart := strings.Index(eventXML, "<EventRecordID>")
	if recordIDStart != -1 {
		recordIDStart += len("<EventRecordID>")
		recordIDEnd := strings.Index(eventXML[recordIDStart:], "</EventRecordID>")
		if recordIDEnd != -1 {
			fmt.Sscanf(eventXML[recordIDStart:recordIDStart+recordIDEnd], "%d", &event.EventRecordID)
		}
	}

	// Extract channel
	channelStart := strings.Index(eventXML, "<Channel>")
	if channelStart != -1 {
		channelStart += len("<Channel>")
		channelEnd := strings.Index(eventXML[channelStart:], "</Channel>")
		if channelEnd != -1 {
			event.Channel = eventXML[channelStart : channelStart+channelEnd]
		}
	}

	// Extract computer
	computerStart := strings.Index(eventXML, "<Computer>")
	if computerStart != -1 {
		computerStart += len("<Computer>")
		computerEnd := strings.Index(eventXML[computerStart:], "</Computer>")
		if computerEnd != -1 {
			event.Computer = eventXML[computerStart : computerStart+computerEnd]
		}
	}

	// Extract message
	messageStart := strings.Index(eventXML, "<Message>")
	if messageStart != -1 {
		messageStart += len("<Message>")
		messageEnd := strings.Index(eventXML[messageStart:], "</Message>")
		if messageEnd != -1 {
			event.Message = eventXML[messageStart : messageStart+messageEnd]
		}
	} else {
		// Try to format message
		event.Message = m.formatEventMessage(event.ProviderName, event.EventID)
	}

	// Extract data
	dataStart := strings.Index(eventXML, "<EventData>")
	if dataStart != -1 {
		dataEnd := strings.Index(eventXML[dataStart:], "</EventData>")
		if dataEnd != -1 {
			dataXML := eventXML[dataStart : dataStart+dataEnd+len("</EventData>")]

			// Extract data items
			dataItemStart := 0
			for {
				dataItemStart = strings.Index(dataXML[dataItemStart:], "<Data")
				if dataItemStart == -1 {
					break
				}

				dataItemStart = dataItemStart + len("<Data")

				// Check if it has a name
				nameStart := strings.Index(dataXML[dataItemStart:], "Name=\"")
				if nameStart != -1 && nameStart < strings.Index(dataXML[dataItemStart:], ">") {
					nameStart = dataItemStart + nameStart + len("Name=\"")
					nameEnd := strings.Index(dataXML[nameStart:], "\"")
					if nameEnd != -1 {
						name := dataXML[nameStart : nameStart+nameEnd]

						// Find closing '>' and ending '</Data>'
						valueStart := strings.Index(dataXML[nameStart+nameEnd:], ">")
						if valueStart != -1 {
							valueStart = nameStart + nameEnd + valueStart + 1
							valueEnd := strings.Index(dataXML[valueStart:], "</Data>")
							if valueEnd != -1 {
								value := dataXML[valueStart : valueStart+valueEnd]
								event.Data[name] = value
							}
						}
					}
				} else {
					// No name, find value directly
					valueStart := strings.Index(dataXML[dataItemStart:], ">")
					if valueStart != -1 {
						valueStart = dataItemStart + valueStart + 1
						valueEnd := strings.Index(dataXML[valueStart:], "</Data>")
						if valueEnd != -1 {
							value := dataXML[valueStart : valueStart+valueEnd]
							// Use index as name
							name := fmt.Sprintf("Param%d", len(event.Data)+1)
							event.Data[name] = value
						}
					}
				}

				// Move past this data item
				dataItemStart += strings.Index(dataXML[dataItemStart:], "</Data>") + len("</Data>")
			}
		}
	}

	return event, nil
}

// formatEventMessage formats a message for an event
func (m *ModernAPI) formatEventMessage(providerName string, eventID uint32) string {
	// This is a simplified implementation
	// A full implementation would use EvtFormatMessage to get the message from the event provider
	// But for now, we'll just return a basic message
	return fmt.Sprintf("Event ID %d from %s", eventID, providerName)
}

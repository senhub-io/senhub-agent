//go:build windows

package eventlog

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LegacyAPI implements the legacy Windows Event Log API (pre-Vista)
type LegacyAPI struct {
	includeXML bool
	mutex      sync.Mutex
}

// Register the legacy API implementation
func init() {
	RegisterAPI("legacy", NewLegacyAPI)
}

// NewLegacyAPI creates a new instance of the Legacy Windows Event Log API
func NewLegacyAPI(includeXML bool) (API, error) {
	return &LegacyAPI{
		includeXML: includeXML,
	}, nil
}

// DLL and procedure references for the Windows Event Log API
var (
	advapi32 = windows.NewLazySystemDLL("advapi32.dll")
	
	procOpenEventLog      = advapi32.NewProc("OpenEventLogW")
	procCloseEventLog     = advapi32.NewProc("CloseEventLog")
	procReadEventLog      = advapi32.NewProc("ReadEventLogW")
	procGetNumberOfEventLogRecords = advapi32.NewProc("GetNumberOfEventLogRecords")
	procGetOldestEventLogRecord = advapi32.NewProc("GetOldestEventLogRecord")
	procReportEvent       = advapi32.NewProc("ReportEventW")
	procClearEventLog     = advapi32.NewProc("ClearEventLogW")
	procBackupEventLog    = advapi32.NewProc("BackupEventLogW")
	procRegisterEventSource = advapi32.NewProc("RegisterEventSourceW")
	procDeregisterEventSource = advapi32.NewProc("DeregisterEventSource")
	procNotifyChangeEventLog = advapi32.NewProc("NotifyChangeEventLog")
)

// Legacy API constants
const (
	EVENTLOG_SEQUENTIAL_READ = 0x0001
	EVENTLOG_SEEK_READ       = 0x0002
	EVENTLOG_FORWARDS_READ   = 0x0004
	EVENTLOG_BACKWARDS_READ  = 0x0008
	
	EVENTLOG_SUCCESS          = 0x0000
	EVENTLOG_ERROR_TYPE       = 0x0001
	EVENTLOG_WARNING_TYPE     = 0x0002
	EVENTLOG_INFORMATION_TYPE = 0x0004
	EVENTLOG_AUDIT_SUCCESS    = 0x0008
	EVENTLOG_AUDIT_FAILURE    = 0x0010
)

// EVENTLOGRECORD represents the legacy event log record structure
type EVENTLOGRECORD struct {
	Length              uint32
	Reserved            uint32
	RecordNumber        uint32
	TimeGenerated       uint32
	TimeWritten         uint32
	EventID             uint32
	EventType           uint16
	NumStrings          uint16
	EventCategory       uint16
	ReservedFlags       uint16
	ClosingRecordNumber uint32
	StringOffset        uint32
	UserSidLength       uint32
	UserSidOffset       uint32
	DataLength          uint32
	DataOffset          uint32
}

// Name returns the API name
func (l *LegacyAPI) Name() string {
	return "Legacy Windows Event Log API (pre-Vista)"
}

// IsAvailable checks if the legacy API is available
func (l *LegacyAPI) IsAvailable() bool {
	// Try to open a system event log
	h, _, _ := procOpenEventLog.Call(
		0, // Local machine
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("System"))),
	)
	
	if h != 0 {
		// Close handle and return true
		procCloseEventLog.Call(h)
		return true
	}
	
	return false
}

// Open opens a channel for reading
func (l *LegacyAPI) Open(channel string) (windows.Handle, error) {
	channelPtr, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return 0, fmt.Errorf("failed to convert channel name: %w", err)
	}
	
	handle, _, err := procOpenEventLog.Call(
		0, // Server (0 = local machine)
		uintptr(unsafe.Pointer(channelPtr)),
	)
	
	if handle == 0 {
		return 0, fmt.Errorf("OpenEventLog failed for channel %s: %v", channel, err)
	}
	
	return windows.Handle(handle), nil
}

// Close closes a channel handle
func (l *LegacyAPI) Close(handle windows.Handle) error {
	if handle == 0 {
		return nil
	}
	
	ret, _, _ := procCloseEventLog.Call(uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("CloseEventLog failed for handle %d", handle)
	}
	
	return nil
}

// Read reads events from a channel
func (l *LegacyAPI) Read(ctx context.Context, handle windows.Handle, maxEvents int, cp *Checkpoint) (*EventBatch, error) {
	if handle == 0 {
		return nil, fmt.Errorf("invalid handle")
	}
	
	// Create batch
	batch := &EventBatch{
		Channel: cp.Channel,
		Events:  make([]Event, 0, maxEvents),
	}
	
	// Get number of records
	var numRecords, oldestRecord uint32
	
	ret, _, err := procGetNumberOfEventLogRecords.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&numRecords)),
	)
	
	if ret == 0 {
		return nil, fmt.Errorf("GetNumberOfEventLogRecords failed: %v", err)
	}
	
	ret, _, err = procGetOldestEventLogRecord.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&oldestRecord)),
	)
	
	if ret == 0 {
		return nil, fmt.Errorf("GetOldestEventLogRecord failed: %v", err)
	}
	
	// If no records, return empty batch
	if numRecords == 0 {
		return batch, nil
	}
	
	// Determine the starting record number
	var firstRecord uint32
	if cp.Position == 0 {
		// No checkpoint, start from the oldest record
		firstRecord = oldestRecord
	} else {
		// Start from the next record after the checkpoint
		firstRecord = uint32(cp.Position) + 1
	}
	
	// Make sure the record number is valid
	if firstRecord < oldestRecord {
		// Log rotation occurred, use oldest available
		firstRecord = oldestRecord
	}
	
	// Calculate the end record number
	lastRecord := oldestRecord + numRecords - 1
	
	// Check if we already processed all records
	if firstRecord > lastRecord {
		return batch, nil
	}
	
	// Determine how many records to read
	recordsToRead := lastRecord - firstRecord + 1
	if recordsToRead > uint32(maxEvents) {
		recordsToRead = uint32(maxEvents)
	}
	
	// Buffer size for reading events
	// Start with a reasonable size and adjust if needed
	bufferSize := uint32(8192) // 8KB initial buffer
	buffer := make([]byte, bufferSize)
	
	for i := uint32(0); i < recordsToRead; i++ {
		select {
		case <-ctx.Done():
			return batch, ctx.Err()
		default:
			// Continue processing
		}
		
		recordNum := firstRecord + i
		
		// Read event flags:
		// - EVENTLOG_SEEK_READ: Start at a specific record
		// - EVENTLOG_FORWARDS_READ: Read in chronological order
		flags := EVENTLOG_SEEK_READ | EVENTLOG_FORWARDS_READ
		
		var bytesRead, bytesNeeded uint32
		
		for {
			ret, _, err := procReadEventLog.Call(
				uintptr(handle),
				uintptr(flags),
				uintptr(recordNum),
				uintptr(unsafe.Pointer(&buffer[0])),
				uintptr(bufferSize),
				uintptr(unsafe.Pointer(&bytesRead)),
				uintptr(unsafe.Pointer(&bytesNeeded)),
			)
			
			if ret != 0 {
				// Success
				break
			}
			
			// Check for errors
			if windows.GetLastError() == windows.ERROR_INSUFFICIENT_BUFFER {
				// Buffer too small, resize and try again
				bufferSize = bytesNeeded
				buffer = make([]byte, bufferSize)
				continue
			}
			
			// If the record is not found, skip to the next one
			if windows.GetLastError() == windows.ERROR_HANDLE_EOF {
				break
			}
			
			// Other error, skip this record
			break
		}
		
		if bytesRead == 0 {
			// No data read, continue to next record
			continue
		}
		
		// Parse the record
		event, err := l.parseEventLogRecord(buffer, bytesRead, cp.Channel)
		if err != nil {
			// Log error but continue with next record
			fmt.Printf("Error parsing event record: %v\n", err)
			continue
		}
		
		// Add to batch
		batch.Events = append(batch.Events, event)
		batch.Position = uint64(recordNum)
	}
	
	return batch, nil
}

// SubscribeToEvents subscribes to events from a channel
func (l *LegacyAPI) SubscribeToEvents(ctx context.Context, channel string, cp *Checkpoint) (<-chan *EventBatch, <-chan error) {
	// Create channels
	eventChan := make(chan *EventBatch, 10)
	errChan := make(chan error, 1)
	
	// Start subscription in a goroutine
	go func() {
		defer close(eventChan)
		defer close(errChan)
		
		// Open the event log
		handle, err := l.Open(channel)
		if err != nil {
			errChan <- fmt.Errorf("failed to open event log: %w", err)
			return
		}
		defer l.Close(handle)
		
		// Create an event for notification
		notifyEvent, err := windows.CreateEvent(nil, 0, 0, nil)
		if err != nil {
			errChan <- fmt.Errorf("failed to create event: %w", err)
			return
		}
		defer windows.CloseHandle(notifyEvent)
		
		// Register for notifications
		ret, _, err := procNotifyChangeEventLog.Call(
			uintptr(handle),
			uintptr(notifyEvent),
		)
		
		if ret == 0 {
			errChan <- fmt.Errorf("NotifyChangeEventLog failed: %v", err)
			return
		}
		
		// Get the initial set of events
		initialBatch, err := l.Read(ctx, handle, 100, cp)
		if err != nil {
			errChan <- fmt.Errorf("failed to read initial events: %w", err)
			return
		}
		
		// Send initial batch if it has events
		if len(initialBatch.Events) > 0 {
			select {
			case eventChan <- initialBatch:
				// Successfully sent
			case <-ctx.Done():
				return
			}
			
			// Update checkpoint
			cp.Position = initialBatch.Position
		}
		
		// Poll for new events
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
				
			case <-ticker.C:
				// Wait for notification or timeout
				waitResult, err := windows.WaitForSingleObject(notifyEvent, 0) // Non-blocking check
				if err != nil {
					errChan <- fmt.Errorf("WaitForSingleObject failed: %w", err)
					continue
				}
				
				if waitResult == windows.WAIT_OBJECT_0 {
					// Event was signaled, read new events
					batch, err := l.Read(ctx, handle, 100, cp)
					if err != nil {
						errChan <- fmt.Errorf("failed to read events after notification: %w", err)
						continue
					}
					
					// Send batch if it has events
					if len(batch.Events) > 0 {
						select {
						case eventChan <- batch:
							// Successfully sent
							cp.Position = batch.Position
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()
	
	return eventChan, errChan
}

// ListChannels lists available event log channels
func (l *LegacyAPI) ListChannels(ctx context.Context) ([]string, error) {
	// Standard event logs available on most Windows systems
	defaultChannels := []string{
		"Application",
		"System",
		"Security",
	}
	
	return defaultChannels, nil
}

// parseEventLogRecord parses a legacy event log record
func (l *LegacyAPI) parseEventLogRecord(buffer []byte, bytesRead uint32, channel string) (Event, error) {
	if bytesRead < uint32(unsafe.Sizeof(EVENTLOGRECORD{})) {
		return Event{}, fmt.Errorf("buffer too small to contain EVENTLOGRECORD")
	}
	
	// Parse the record header
	record := (*EVENTLOGRECORD)(unsafe.Pointer(&buffer[0]))
	
	// Extract source name
	sourceNamePtr := uintptr(unsafe.Pointer(&buffer[0])) + uintptr(record.StringOffset)
	sourceName := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(sourceNamePtr)))
	
	// Extract timestamp
	timeGenerated := time.Unix(int64(record.TimeGenerated), 0)
	
	// Extract strings
	strings := make([]string, record.NumStrings)
	stringsPtr := sourceNamePtr + uintptr(len(sourceName)*2) + 2 // Skip null terminator
	
	for i := uint16(0); i < record.NumStrings; i++ {
		strings[i] = windows.UTF16PtrToString((*uint16)(unsafe.Pointer(stringsPtr)))
		stringsPtr += uintptr(len(strings[i])*2) + 2 // Skip null terminator
	}
	
	// Extract binary data
	var binaryData []byte
	if record.DataLength > 0 {
		binaryData = make([]byte, record.DataLength)
		copy(binaryData, buffer[record.DataOffset:record.DataOffset+record.DataLength])
	}
	
	// Convert level to standard format
	var level EventLevel
	switch record.EventType {
	case EVENTLOG_ERROR_TYPE:
		level = EventLevelError
	case EVENTLOG_WARNING_TYPE:
		level = EventLevelWarning
	case EVENTLOG_INFORMATION_TYPE:
		level = EventLevelInformation
	case EVENTLOG_AUDIT_SUCCESS:
		level = EventLevelInformation
	case EVENTLOG_AUDIT_FAILURE:
		level = EventLevelError
	default:
		level = EventLevelInformation
	}
	
	// Format message
	message := l.formatEventMessage(strings)
	
	// Create an Event object
	event := Event{
		ProviderName:   sourceName,
		EventID:        record.EventID & 0xFFFF, // Mask off the facility and reserved bits
		Level:          level,
		TimeCreated:    timeGenerated,
		EventRecordID:  uint64(record.RecordNumber),
		Channel:        channel,
		Computer:       "", // Not available in legacy format
		Message:        message,
		Data:           make(map[string]string),
	}
	
	// Add string parameters to data
	for i, str := range strings {
		event.Data[fmt.Sprintf("Param%d", i+1)] = str
	}
	
	return event, nil
}

// formatEventMessage creates a message from parameter strings
func (l *LegacyAPI) formatEventMessage(params []string) string {
	// Simple formatting - just join the strings
	// A more sophisticated implementation would use FormatMessage API
	// to properly insert the parameters into the message template
	
	if len(params) == 0 {
		return ""
	}
	
	if len(params) == 1 {
		return params[0]
	}
	
	// Join all parameters
	result := params[0]
	
	for i := 1; i < len(params); i++ {
		result += " - " + params[i]
	}
	
	return result
}
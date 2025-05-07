//go:build windows
// +build windows

package main

import (
	"fmt"
	"log"
	"syscall"
	"unsafe"
	"time"
	"runtime"
	"os"
)

var (
	// Windows API DLLs
	modwevtapi = syscall.NewLazyDLL("wevtapi.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")
	
	// Windows API functions
	procEvtSubscribe = modwevtapi.NewProc("EvtSubscribe")
	procEvtNext = modwevtapi.NewProc("EvtNext")
	procEvtRender = modwevtapi.NewProc("EvtRender")
	procEvtClose = modwevtapi.NewProc("EvtClose")
	procEvtFormatMessage = modwevtapi.NewProc("EvtFormatMessage")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
)

// Constants
const (
	EvtSubscribeToFutureEvents uint32 = 1
	EvtSubscribeStartAtOldestRecord uint32 = 2
	
	EvtRenderEventValues uint32 = 0
	EvtRenderEventXml uint32 = 1
	
	EvtFormatMessageEvent uint32 = 1
	
	EvtSystemProviderName uint32 = 2
	EvtSystemEventID uint32 = 7
	EvtSystemLevel uint32 = 8
	EvtSystemTimeCreated uint32 = 14
)

// EventHandle type for Windows Event handles
type EventHandle uintptr

// Global variables to track events
var eventCount = 0

// printError formats and prints an error with Windows error code
func printError(msg string, err error) {
	if errno, ok := err.(syscall.Errno); ok {
		fmt.Printf("ERROR: %s: %v (Error code: %d/0x%x)\n", msg, err, errno, errno)
	} else {
		fmt.Printf("ERROR: %s: %v\n", msg, err)
	}
}

// closeHandle safely closes a Windows event handle
func closeHandle(handle EventHandle) {
	if handle != 0 {
		procEvtClose.Call(uintptr(handle))
	}
}

// formatEventMessage formats the message for an event
func formatEventMessage(providerName string, eventHandle EventHandle) string {
	// Convert provider name to UTF16
	providerNamePtr, _ := syscall.UTF16PtrFromString(providerName)
	
	// Open publisher metadata
	publisherHandle, _, _ := procEvtOpenPublisherMetadata.Call(
		0, // Session
		uintptr(unsafe.Pointer(providerNamePtr)),
		0, // Locale
		0, // Flags
	)
	
	if publisherHandle == 0 {
		return "[Error opening publisher metadata]"
	}
	defer procEvtClose.Call(publisherHandle)
	
	var bufferUsed uint32
	
	// First call to get buffer size
	ret, _, _ := procEvtFormatMessage.Call(
		publisherHandle,
		uintptr(eventHandle),
		0, // MessageId
		0, // ValueCount
		0, // Values
		uintptr(EvtFormatMessageEvent),
		0, // BufferSize
		0, // Buffer
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret != 0 || bufferUsed == 0 {
		return "[No message]"
	}
	
	// Allocate buffer
	buffer := make([]uint16, bufferUsed)
	
	// Second call with proper buffer
	ret, _, _ = procEvtFormatMessage.Call(
		publisherHandle,
		uintptr(eventHandle),
		0, // MessageId
		0, // ValueCount
		0, // Values
		uintptr(EvtFormatMessageEvent),
		uintptr(bufferUsed*2), // Buffer size in bytes
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferUsed)),
	)
	
	if ret == 0 {
		return "[Error formatting message]"
	}
	
	return syscall.UTF16ToString(buffer)
}

// extractProviderName gets the provider name from event data
func extractProviderName(eventValues []byte) string {
	if len(eventValues) < 60 {
		return "Unknown Provider"
	}
	
	// Extract provider name pointer
	offset := EvtSystemProviderName * 16
	providerPtr := *(*uintptr)(unsafe.Pointer(&eventValues[offset]))
	
	if providerPtr == 0 {
		return "Unknown Provider"
	}
	
	// Get string length
	length := 0
	for i := 0; ; i += 2 {
		if *(*uint16)(unsafe.Pointer(providerPtr + uintptr(i))) == 0 {
			length = i / 2
			break
		}
	}
	
	// Convert to string
	return syscall.UTF16ToString((*[1024]uint16)(unsafe.Pointer(providerPtr))[:length:length])
}

// extractEventID gets the event ID from event data
func extractEventID(eventValues []byte) uint32 {
	if len(eventValues) < (EvtSystemEventID*16 + 8) {
		return 0
	}
	
	offset := EvtSystemEventID * 16
	return *(*uint32)(unsafe.Pointer(&eventValues[offset]))
}

// processEvents processes events from a subscription
func processEvents(subscriptionHandle, signalEvent EventHandle) {
	fmt.Println("Starting event processing loop...")
	
	for {
		// Wait for events (100ms timeout)
		waitResult, _, _ := syscall.WaitForSingleObject(syscall.Handle(signalEvent), 100)
		
		// Check if we should exit
		select {
		default:
			// Continue processing
		}
		
		// If not signaled, continue waiting
		if waitResult != syscall.WAIT_OBJECT_0 {
			continue
		}
		
		// Process all available events
		for {
			// Get next batch of events
			var eventsReturned uint32
			eventArray := make([]EventHandle, 10)
			
			ret, _, err := procEvtNext.Call(
				uintptr(subscriptionHandle),
				10, // Max events
				uintptr(unsafe.Pointer(&eventArray[0])),
				0, // Timeout
				0, // Flags
				uintptr(unsafe.Pointer(&eventsReturned)),
			)
			
			// If no events or error, break
			if ret == 0 {
				if err != syscall.ERROR_NO_MORE_ITEMS {
					printError("EvtNext failed", err)
				}
				break
			}
			
			// Process each event
			for i := uint32(0); i < eventsReturned; i++ {
				eventHandle := eventArray[i]
				if eventHandle == 0 {
					continue
				}
				
				// Increment event count
				eventCount++
				
				// Get event values
				var bufferUsed, propertyCount uint32
				
				// First call to get buffer size
				ret, _, _ := procEvtRender.Call(
					0, // Context
					uintptr(eventHandle),
					uintptr(EvtRenderEventValues),
					0, // BufferSize
					0, // Buffer
					uintptr(unsafe.Pointer(&bufferUsed)),
					uintptr(unsafe.Pointer(&propertyCount)),
				)
				
				if ret == 0 || bufferUsed == 0 {
					fmt.Println("Failed to get event values buffer size")
					closeHandle(eventHandle)
					continue
				}
				
				// Allocate buffer
				buffer := make([]byte, bufferUsed)
				
				// Second call with proper buffer
				ret, _, _ = procEvtRender.Call(
					0, // Context
					uintptr(eventHandle),
					uintptr(EvtRenderEventValues),
					uintptr(bufferUsed),
					uintptr(unsafe.Pointer(&buffer[0])),
					uintptr(unsafe.Pointer(&bufferUsed)),
					uintptr(unsafe.Pointer(&propertyCount)),
				)
				
				if ret == 0 {
					fmt.Println("Failed to render event values")
					closeHandle(eventHandle)
					continue
				}
				
				// Extract event data
				providerName := extractProviderName(buffer)
				eventID := extractEventID(buffer)
				
				// Get message
				message := formatEventMessage(providerName, eventHandle)
				
				// Print event info
				fmt.Printf("\n========== EVENT #%d ==========\n", eventCount)
				fmt.Printf("Provider: %s\n", providerName)
				fmt.Printf("EventID: %d\n", eventID)
				fmt.Printf("Message: %s\n", message)
				fmt.Println("===============================")
				
				// Close event handle
				closeHandle(eventHandle)
				
				// Specifically, check for EventID 1234
				if eventID == 1234 {
					fmt.Printf("\n!!! DETECTED EventID 1234 FROM %s !!!\n\n", providerName)
				}
			}
		}
	}
}

func main() {
	fmt.Println("=================================================")
	fmt.Println("MINIMAL WINDOWS EVENT LOG SUBSCRIPTION TEST")
	fmt.Println("=================================================")
	
	// Check if running as administrator
	isAdmin := checkAdmin()
	fmt.Printf("Running as administrator: %v\n", isAdmin)
	if !isAdmin {
		fmt.Println("WARNING: This program may not work correctly without admin privileges")
	}
	
	// Print system info
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Architecture: %s\n", runtime.GOARCH)
	fmt.Println()
	
	// Define channel name
	channelName, err := syscall.UTF16PtrFromString("Application")
	if err != nil {
		log.Fatalf("Failed to convert channel name: %v", err)
	}
	
	// Create event for signaling
	event, err := syscall.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		log.Fatalf("Failed to create event: %v", err)
	}
	fmt.Printf("Created signal event handle: %d\n", event)
	
	// Try both subscription flags
	fmt.Println("\nAttempting subscription with EvtSubscribeStartAtOldestRecord...")
	flags := EvtSubscribeStartAtOldestRecord
	
	// Call EvtSubscribe
	ret, _, err := procEvtSubscribe.Call(
		0, // Session (NULL = local machine)
		uintptr(event), // SignalEvent
		uintptr(unsafe.Pointer(channelName)), // Channel name
		0, // Query (NULL = all events)
		0, // Bookmark (NULL = no bookmark)
		0, // Context
		0, // No callback function, using polling model
		uintptr(flags), // Flags
	)
	
	if ret == 0 {
		fmt.Printf("EvtSubscribe failed: %v (Code: %d)\n", err, syscall.GetLastError())
		fmt.Println("\nTrying with EvtSubscribeToFutureEvents instead...")
		
		// Try with future events flag
		flags = EvtSubscribeToFutureEvents
		ret, _, err = procEvtSubscribe.Call(
			0, // Session
			uintptr(event), // SignalEvent
			uintptr(unsafe.Pointer(channelName)), // Channel name
			0, // Query
			0, // Bookmark
			0, // Context
			0, // No callback
			uintptr(flags), // Flags
		)
		
		if ret == 0 {
			log.Fatalf("All subscription attempts failed: %v (Code: %d)", err, syscall.GetLastError())
		}
	}
	
	subscriptionHandle := EventHandle(ret)
	fmt.Printf("Subscription successful. Handle: %d\n", subscriptionHandle)
	defer closeHandle(subscriptionHandle)
	
	// Start event processing in background
	go processEvents(subscriptionHandle, EventHandle(event))
	
	// Print instructions
	fmt.Println("\nWaiting for events (60s)...")
	fmt.Println("Try generating an event with:")
	fmt.Println("Write-EventLog -LogName Application -Source \"Application\" -EventId 1234 -EntryType Information -Message \"Test event\"")
	fmt.Println("=================================================")
	
	// Wait for events
	start := time.Now()
	for time.Since(start) < 60*time.Second {
		time.Sleep(1 * time.Second)
		
		// Print a heartbeat every 5 seconds
		if int(time.Since(start).Seconds())%5 == 0 {
			fmt.Printf("Still listening... (Time remaining: %d seconds, Events received: %d)\n", 
				60-int(time.Since(start).Seconds()), eventCount)
		}
	}
	
	// Print summary
	fmt.Println("\n=================================================")
	fmt.Printf("Test completed. Total events received: %d\n", eventCount)
	if eventCount == 0 {
		fmt.Println("\nTROUBLESHOOTING TIPS:")
		fmt.Println("1. Make sure you're running as Administrator")
		fmt.Println("2. Verify Windows Event Log service is running")
		fmt.Println("3. Try generating events with correct permissions")
		fmt.Println("4. Check Event Viewer to confirm events are being logged")
	}
	fmt.Println("=================================================")
}

// checkAdmin determines if the process is running with administrator privileges
func checkAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}
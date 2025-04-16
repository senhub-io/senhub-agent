//go:build windows

package eventlog

import (
	"time"
)

// EventLevel represents the severity level of a Windows event
type EventLevel uint8

// Standard Windows event levels
const (
	EventLevelLogAlways  EventLevel = 0
	EventLevelCritical   EventLevel = 1
	EventLevelError      EventLevel = 2
	EventLevelWarning    EventLevel = 3
	EventLevelInformation EventLevel = 4
	EventLevelVerbose    EventLevel = 5
)

// Event represents a Windows event log entry
type Event struct {
	ProviderName       string            // Source of the event
	ProviderGUID       string            // Provider GUID
	EventID            uint32            // Event identifier
	Version            uint16            // Event version
	Level              EventLevel        // Severity level
	Task               uint16            // Task category
	Opcode             uint8             // Operation code
	Keywords           uint64            // Event keywords
	TimeCreated        time.Time         // When the event occurred
	EventRecordID      uint64            // Record identifier
	ActivityID         string            // Activity correlation ID
	RelatedActivityID  string            // Related activity correlation ID
	ProcessID          uint32            // Process that generated the event
	ThreadID           uint32            // Thread that generated the event
	Channel            string            // Event channel
	Computer           string            // Computer name
	UserID             string            // Security identifier
	Message            string            // Formatted message
	RawXML             string            // Raw event XML (optional)
	Data               map[string]string // Structured event data
}

// EventBatch represents a collection of events
type EventBatch struct {
	Channel  string  // Channel source
	Events   []Event // Collection of events
	Position uint64  // Current position (bookmark)
}

// Checkpoint represents a resumption point in the event log
type Checkpoint struct {
	Channel      string    // Event channel name
	Position     uint64    // Record ID or other position indicator
	Timestamp    time.Time // Last timestamp processed
	LastModified time.Time // When the checkpoint was last updated
	BookmarkXML  string    // Raw bookmark XML for Windows API
}

// SubscriptionFlags defines the subscription mode
type SubscriptionFlags uint32

// Subscription modes
const (
	EvtSubscribeToFutureEvents SubscriptionFlags = 1
	EvtSubscribeStartAtOldestRecord SubscriptionFlags = 2
	EvtSubscribeStartAfterBookmark SubscriptionFlags = 3
)

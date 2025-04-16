//go:build windows

package eventlog

// Constants for Windows Event Log API
const (
	// Event Subscription Flags
	EvtSubscribeToFutureEvents   = 1
	EvtSubscribeStartAtOldestRecord = 2
	EvtSubscribeStartAfterBookmark = 3

	// Render Flags
	EvtRenderEventValues = 0
	EvtRenderEventXml    = 1
	EvtRenderBookmark    = 2

	// Format Message Flags
	EvtFormatMessageEvent    = 1
	EvtFormatMessageLevel    = 2
	EvtFormatMessageTask     = 3
	EvtFormatMessageOpcode   = 4
	EvtFormatMessageKeyword  = 5
	EvtFormatMessageChannel  = 6
	EvtFormatMessageProvider = 7
	EvtFormatMessageId       = 8
	EvtFormatMessageXml      = 9

	// System Event Properties
	EvtSystemProviderName      = 2
	EvtSystemEventID           = 7
	EvtSystemQualifiers        = 8
	EvtSystemLevel             = 9
	EvtSystemTask              = 10
	EvtSystemOpcode            = 11
	EvtSystemKeywords          = 12
	EvtSystemTimeCreated       = 13
	EvtSystemEventRecordId     = 14
	EvtSystemActivityID        = 15
	EvtSystemRelatedActivityID = 16
	EvtSystemProcessID         = 17
	EvtSystemThreadID          = 18
	EvtSystemChannel           = 19
	EvtSystemComputer          = 20
	EvtSystemUserID            = 21
	EvtSystemVersion           = 22

	// Wait Constants
	WAIT_OBJECT_0 = 0
	WAIT_TIMEOUT  = 0x00000102

	// Error Codes
	ERROR_SUCCESS           = 0
	ERROR_INSUFFICIENT_BUFFER = 122
	ERROR_NO_MORE_ITEMS      = 259
	ERROR_EVT_MESSAGE_NOT_FOUND = 15027
	ERROR_EVT_CHANNEL_NOT_FOUND = 15007
	ERROR_EVT_INVALID_CHANNEL_PATH = 15001
	ERROR_EVT_INVALID_QUERY = 15001
	ERROR_ACCESS_DENIED = 5
	ERROR_INVALID_HANDLE = 6
	ERROR_INVALID_PARAMETER = 87
)

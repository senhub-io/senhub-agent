//go:build windows

package eventlog

// Constants for Windows Event Log API
const (
	// Event Subscription Flags
	EvtSubscribeToFutureEvents   uint32 = 1
	EvtSubscribeStartAtOldestRecord uint32 = 2
	EvtSubscribeStartAfterBookmark uint32 = 3

	// Render Flags
	EvtRenderEventValues uint32 = 0
	EvtRenderEventXml    uint32 = 1
	EvtRenderBookmark    uint32 = 2

	// Format Message Flags
	EvtFormatMessageEvent    uint32 = 1
	EvtFormatMessageLevel    uint32 = 2
	EvtFormatMessageTask     uint32 = 3
	EvtFormatMessageOpcode   uint32 = 4
	EvtFormatMessageKeyword  uint32 = 5
	EvtFormatMessageChannel  uint32 = 6
	EvtFormatMessageProvider uint32 = 7
	EvtFormatMessageId       uint32 = 8
	EvtFormatMessageXml      uint32 = 9

	// System Event Properties
	EvtSystemProviderName      uint32 = 2
	EvtSystemEventID           uint32 = 7
	EvtSystemQualifiers        uint32 = 8
	EvtSystemLevel             uint32 = 9
	EvtSystemTask              uint32 = 10
	EvtSystemOpcode            uint32 = 11
	EvtSystemKeywords          uint32 = 12
	EvtSystemTimeCreated       uint32 = 13
	EvtSystemEventRecordId     uint32 = 14
	EvtSystemActivityID        uint32 = 15
	EvtSystemRelatedActivityID uint32 = 16
	EvtSystemProcessID         uint32 = 17
	EvtSystemThreadID          uint32 = 18
	EvtSystemChannel           uint32 = 19
	EvtSystemComputer          uint32 = 20
	EvtSystemUserID            uint32 = 21
	EvtSystemVersion           uint32 = 22

	// Wait Constants
	WAIT_OBJECT_0 uint32 = 0
	WAIT_TIMEOUT  uint32 = 0x00000102

	// Error Codes
	ERROR_SUCCESS              uint32 = 0
	ERROR_INSUFFICIENT_BUFFER  uint32 = 122
	ERROR_NO_MORE_ITEMS        uint32 = 259
	ERROR_EVT_MESSAGE_NOT_FOUND uint32 = 15027
	ERROR_EVT_CHANNEL_NOT_FOUND uint32 = 15007
	ERROR_EVT_INVALID_CHANNEL_PATH uint32 = 15001
	ERROR_EVT_INVALID_QUERY    uint32 = 15001
	ERROR_ACCESS_DENIED        uint32 = 5
	ERROR_INVALID_HANDLE       uint32 = 6
	ERROR_INVALID_PARAMETER    uint32 = 87
)

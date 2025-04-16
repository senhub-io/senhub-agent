//go:build windows

package eventlog

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// XML utilities 

// ExtractSimpleXMLValue extracts a value from an XML element using a simple approach
func ExtractSimpleXMLValue(xmlContent, tagName string) string {
	startTag := "<" + tagName + ">"
	endTag := "</" + tagName + ">"
	
	startIndex := strings.Index(xmlContent, startTag)
	if startIndex == -1 {
		return ""
	}
	
	startIndex += len(startTag)
	endIndex := strings.Index(xmlContent[startIndex:], endTag)
	if endIndex == -1 {
		return ""
	}
	
	return xmlContent[startIndex : startIndex+endIndex]
}

// ExtractAttributeXMLValue extracts an attribute value from an XML element
func ExtractAttributeXMLValue(xmlContent, attribute string) string {
	attrString := attribute + "=\""
	startIndex := strings.Index(xmlContent, attrString)
	if startIndex == -1 {
		return ""
	}
	
	startIndex += len(attrString)
	endIndex := strings.Index(xmlContent[startIndex:], "\"")
	if endIndex == -1 {
		return ""
	}
	
	return xmlContent[startIndex : startIndex+endIndex]
}

// IsValidXML checks if a string is valid XML
func IsValidXML(content string) bool {
	var xmlTest interface{}
	return xml.Unmarshal([]byte(content), &xmlTest) == nil
}

// UTF16BytesToString converts a UTF-16 byte slice to a string
func UTF16BytesToString(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	
	// Convert bytes to uint16 slice
	var u16s []uint16
	for i := 0; i < len(b); i += 2 {
		if i+1 >= len(b) {
			break
		}
		u := uint16(b[i]) | (uint16(b[i+1]) << 8)
		if u == 0 {
			// Null terminator
			break
		}
		u16s = append(u16s, u)
	}
	
	return windows.UTF16ToString(u16s)
}

// EventDataMap parses event data from XML
func EventDataMap(eventXML string) map[string]string {
	data := make(map[string]string)
	
	// Extract EventData section
	dataStart := strings.Index(eventXML, "<EventData>")
	if dataStart == -1 {
		return data
	}
	
	dataEnd := strings.Index(eventXML[dataStart:], "</EventData>")
	if dataEnd == -1 {
		return data
	}
	
	eventDataXML := eventXML[dataStart : dataStart+dataEnd+len("</EventData>")]
	
	// Extract Data items
	dataPattern := regexp.MustCompile(`<Data(?:\s+Name="([^"]*)")?>(.*?)</Data>`)
	matches := dataPattern.FindAllStringSubmatch(eventDataXML, -1)
	
	for i, match := range matches {
		var name string
		if match[1] != "" {
			name = match[1]
		} else {
			name = fmt.Sprintf("Param%d", i+1)
		}
		
		value := match[2]
		data[name] = value
	}
	
	return data
}

// UTF16FromString converts a string to UTF-16 bytes
func UTF16FromString(s string) []byte {
	// Convert to uint16 slice
	u16 := windows.StringToUTF16(s)
	
	// Convert to bytes
	bytes := make([]byte, len(u16)*2)
	for i, u := range u16 {
		bytes[i*2] = byte(u)
		bytes[i*2+1] = byte(u >> 8)
	}
	
	return bytes
}

// SafeUTF16PtrFromString creates a UTF-16 pointer from a string with error handling
func SafeUTF16PtrFromString(s string) (*uint16, error) {
	return windows.UTF16PtrFromString(s)
}

// TimeCreatedToTime converts a Windows Event time string to time.Time
func TimeCreatedToTime(timeStr string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, timeStr)
}

// FormatWindowsTime formats a Windows FILETIME to RFC3339
func FormatWindowsTime(ft FILETIME) string {
	// Convert FILETIME to time.Time
	// FILETIME is 100-nanosecond intervals since January 1, 1601 UTC
	// We need to convert to Unix time (seconds since January 1, 1970 UTC)
	
	// First, convert to 64-bit value
	nsec := int64(ft.HighDateTime)<<32 + int64(ft.LowDateTime)
	
	// Convert to seconds and nanoseconds for Unix time
	// Windows epoch (1601-01-01) is 11,644,473,600 seconds before Unix epoch (1970-01-01)
	const windowsToUnixOffset = 116444736000000000
	
	// Convert 100ns intervals to nanoseconds
	nsec = (nsec - windowsToUnixOffset) * 100
	
	// Create time.Time value
	sec := nsec / 1000000000
	nsec = nsec % 1000000000
	if nsec < 0 {
		nsec += 1000000000
		sec--
	}
	
	return time.Unix(sec, nsec).Format(time.RFC3339Nano)
}

// SystemtimeToTime converts a SYSTEMTIME to time.Time
func SystemtimeToTime(st SYSTEMTIME) time.Time {
	return time.Date(
		int(st.Year),
		time.Month(st.Month),
		int(st.Day),
		int(st.Hour),
		int(st.Minute),
		int(st.Second),
		int(st.Milliseconds)*1000000, // Convert to nanoseconds
		time.Local,
	)
}

// UintptrToBytes converts a uintptr to a byte slice with specified length
func UintptrToBytes(ptr uintptr, length int) []byte {
	var data []byte
	
	for i := 0; i < length; i++ {
		addr := unsafe.Pointer(ptr + uintptr(i))
		b := *(*byte)(addr)
		data = append(data, b)
	}
	
	return data
}

// ExtractEventXMLStructured extracts structured data from event XML
func ExtractEventXMLStructured(xmlContent string) (map[string]interface{}, error) {
	var xmlEvent struct {
		XMLName     xml.Name `xml:"Event"`
		System      struct {
			Provider struct {
				Name string `xml:"Name,attr"`
				GUID string `xml:"Guid,attr"`
			} `xml:"Provider"`
			EventID      int    `xml:"EventID"`
			Version      int    `xml:"Version"`
			Level        int    `xml:"Level"`
			Task         int    `xml:"Task"`
			Opcode       int    `xml:"Opcode"`
			Keywords     string `xml:"Keywords"`
			TimeCreated  struct {
				SystemTime string `xml:"SystemTime,attr"`
			} `xml:"TimeCreated"`
			EventRecordID int    `xml:"EventRecordID"`
			Channel       string `xml:"Channel"`
			Computer      string `xml:"Computer"`
		} `xml:"System"`
		EventData struct {
			Data []struct {
				Name  string `xml:"Name,attr"`
				Value string `xml:",chardata"`
			} `xml:"Data"`
		} `xml:"EventData"`
		RenderingInfo struct {
			Message string `xml:"Message"`
		} `xml:"RenderingInfo"`
	}
	
	err := xml.Unmarshal([]byte(xmlContent), &xmlEvent)
	if err != nil {
		return nil, err
	}
	
	// Convert to map
	result := make(map[string]interface{})
	
	// System fields
	result["ProviderName"] = xmlEvent.System.Provider.Name
	result["ProviderGUID"] = xmlEvent.System.Provider.GUID
	result["EventID"] = xmlEvent.System.EventID
	result["Version"] = xmlEvent.System.Version
	result["Level"] = xmlEvent.System.Level
	result["Task"] = xmlEvent.System.Task
	result["Opcode"] = xmlEvent.System.Opcode
	result["Keywords"] = xmlEvent.System.Keywords
	result["TimeCreated"] = xmlEvent.System.TimeCreated.SystemTime
	result["EventRecordID"] = xmlEvent.System.EventRecordID
	result["Channel"] = xmlEvent.System.Channel
	result["Computer"] = xmlEvent.System.Computer
	
	// Message
	if xmlEvent.RenderingInfo.Message != "" {
		result["Message"] = xmlEvent.RenderingInfo.Message
	}
	
	// EventData
	eventData := make(map[string]string)
	for _, data := range xmlEvent.EventData.Data {
		if data.Name != "" {
			eventData[data.Name] = data.Value
		}
	}
	result["EventData"] = eventData
	
	return result, nil
}
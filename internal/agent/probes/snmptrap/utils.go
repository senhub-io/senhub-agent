package snmptrap

import (
	"net"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// extractIP extracts the IP address from an address string
func extractIP(addr string) string {
	// addr format is typically "192.168.1.100:45678"
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If splitting fails, return the original address
		return addr
	}
	return host
}

// getTypeString converts SNMP type to string representation
func getTypeString(t gosnmp.Asn1BER) string {
	switch t {
	case gosnmp.Boolean:
		return "Boolean"
	case gosnmp.Integer:
		return "Integer32"
	case gosnmp.BitString:
		return "BitString"
	case gosnmp.OctetString:
		return "OctetString"
	case gosnmp.Null:
		return "Null"
	case gosnmp.ObjectIdentifier:
		return "ObjectIdentifier"
	case gosnmp.ObjectDescription:
		return "ObjectDescription"
	case gosnmp.IPAddress:
		return "IpAddress"
	case gosnmp.Counter32:
		return "Counter32"
	case gosnmp.Gauge32:
		return "Gauge32"
	case gosnmp.TimeTicks:
		return "TimeTicks"
	case gosnmp.Opaque:
		return "Opaque"
	case gosnmp.NsapAddress:
		return "NsapAddress"
	case gosnmp.Counter64:
		return "Counter64"
	case gosnmp.Uinteger32:
		return "Uinteger32"
	case gosnmp.OpaqueFloat:
		return "OpaqueFloat"
	case gosnmp.OpaqueDouble:
		return "OpaqueDouble"
	case gosnmp.NoSuchObject:
		return "NoSuchObject"
	case gosnmp.NoSuchInstance:
		return "NoSuchInstance"
	case gosnmp.EndOfMibView:
		return "EndOfMibView"
	default:
		return "Unknown"
	}
}

// isIPAllowed checks if an IP is allowed based on filter configuration
func isIPAllowed(ip string, allowedSources, blockedSources []string) bool {
	// First check if IP is in blocked list
	for _, blocked := range blockedSources {
		if isIPInNetwork(ip, blocked) {
			return false
		}
	}
	
	// If no allowed list specified, allow all (except blocked)
	if len(allowedSources) == 0 {
		return true
	}
	
	// Check if IP is in allowed list
	for _, allowed := range allowedSources {
		if isIPInNetwork(ip, allowed) {
			return true
		}
	}
	
	return false
}

// isIPInNetwork checks if an IP is in a CIDR network
func isIPInNetwork(ipStr, cidr string) bool {
	// Check if cidr is a single IP or a network
	if !strings.Contains(cidr, "/") {
		// Single IP comparison
		return ipStr == cidr
	}
	
	// Parse CIDR
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	
	// Parse IP
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	
	return network.Contains(ip)
}

// formatValue formats a varbind value for display
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case []byte:
		// Try to convert to string if printable
		if isPrintable(v) {
			return string(v)
		}
		// Otherwise return hex representation
		return formatHex(v)
	case string:
		return v
	default:
		return strings.TrimSpace(formatInterface(v))
	}
}

// isPrintable checks if a byte slice contains printable ASCII characters
func isPrintable(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	
	for _, b := range data {
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}

// formatHex formats bytes as hexadecimal string
func formatHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	
	var result strings.Builder
	result.WriteString("0x")
	for _, b := range data {
		result.WriteString(strings.ToUpper(formatByte(b)))
	}
	return result.String()
}

// formatByte formats a single byte as two hex digits
func formatByte(b byte) string {
	const hexChars = "0123456789abcdef"
	return string([]byte{hexChars[b>>4], hexChars[b&0x0f]})
}

// formatInterface formats any interface value to string
func formatInterface(value interface{}) string {
	switch v := value.(type) {
	case int:
		return formatInt(v)
	case int32:
		return formatInt(int(v))
	case int64:
		return formatInt64(v)
	case uint:
		return formatUint(v)
	case uint32:
		return formatUint(uint(v))
	case uint64:
		return formatUint64(v)
	case float32:
		return formatFloat32(v)
	case float64:
		return formatFloat64(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		// Fallback to simple string conversion
		return interfaceToString(value)
	}
}

// Number formatting functions (avoiding fmt package for performance)
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	
	negative := n < 0
	if negative {
		n = -n
	}
	
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	// Reverse digits
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	
	negative := n < 0
	if negative {
		n = -n
	}
	
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	// Reverse digits
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func formatUint(n uint) string {
	if n == 0 {
		return "0"
	}
	
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	// Reverse digits
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	return string(digits)
}

func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	
	// Reverse digits
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	
	return string(digits)
}

func formatFloat32(f float32) string {
	// Simple float formatting - can be enhanced if needed
	return interfaceToString(f)
}

func formatFloat64(f float64) string {
	// Simple float formatting - can be enhanced if needed
	return interfaceToString(f)
}

// interfaceToString is a fallback string conversion
func interfaceToString(v interface{}) string {
	// This is a simplified version - in production you might want to use fmt.Sprint
	// but we're avoiding it here for performance reasons
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		// For complex types, we'll need to import fmt or implement more converters
		// For now, return a placeholder
		return "<value>"
	}
}
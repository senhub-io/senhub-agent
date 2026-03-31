// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"fmt"
	"net/url"
	"strings"
)

// getFloat safely extracts float64 from interface{}
func getFloat(data map[string]interface{}, key string) float64 {
	if val, ok := data[key]; ok {
		switch v := val.(type) {
		case float64:
			// Check for sentinel value (max uint32 = 2^32 - 1 = 4294967295)
			// Netscaler API uses this to indicate "no data" or "not available"
			// This is a common pattern in embedded systems APIs
			if v >= 4294967295.0 {
				return 0
			}
			return v
		case float32:
			// Same sentinel check for float32
			if v >= 4294967295.0 {
				return 0
			}
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case uint:
			return float64(v)
		case uint32:
			// Sentinel value check for uint32
			if v == 0xFFFFFFFF { // 2^32 - 1
				return 0
			}
			return float64(v)
		case uint64:
			// Sentinel value check for uint64
			if v == 0xFFFFFFFF { // Same sentinel for compatibility
				return 0
			}
			return float64(v)
		case string:
			// Try to parse string to float
			var f float64
			_, _ = fmt.Sscanf(v, "%f", &f)
			// Check sentinel after parsing
			if f >= 4294967295.0 {
				return 0
			}
			return f
		}
	}
	return 0
}

// getString safely extracts string from interface{}
func getString(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// parseNetscalerState converts Citrix ADC NITRO API state strings to official numeric codes
// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
// Returns: UP=7, DOWN=1, UNKNOWN=2, BUSY=3, OUT OF SERVICE=4, TROFS=5 (Transition Out of Service), TROFS_DOWN=8
func parseNetscalerState(state string) float32 {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "UP":
		return 7
	case "DOWN":
		return 1
	case "UNKNOWN":
		return 2
	case "BUSY":
		return 3
	case "OUT OF SERVICE", "OFS":
		return 4
	case "TROFS", "TRANSITION OUT OF SERVICE":
		return 5
	case "TROFS_DOWN":
		return 8
	default:
		return 2 // UNKNOWN for unrecognized states
	}
}

// isBaseURLMatchingIP checks if the probe's base_url hostname literally equals the given IP.
// This does NOT perform DNS resolution — for hostname-based base_urls (e.g. https://ns.example.com),
// matching will return false even if the hostname resolves to that IP.
// Use IP-based base_urls for reliable HA node identification.
func (p *netscalerProbe) isBaseURLMatchingIP(ip string) bool {
	parsed, err := url.Parse(p.baseURL)
	if err != nil {
		return false
	}
	return parsed.Hostname() == ip
}

// parseNetscalerBinaryState converts binary state strings to numeric codes
// Used for interfaces and other resources with simple ENABLED/DISABLED states
// Returns: ENABLED/UP=1, DISABLED/DOWN=0
func parseNetscalerBinaryState(state string) float32 {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "UP", "ENABLED":
		return 1
	case "DOWN", "DISABLED":
		return 0
	default:
		return 0
	}
}

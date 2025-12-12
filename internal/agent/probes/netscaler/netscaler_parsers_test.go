// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"testing"
)

// TestParseNetscalerState tests the parseNetscalerState function with all valid states
func TestParseNetscalerState(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float32
	}{
		// Official Netscaler states
		{"UP state", "UP", 7},
		{"DOWN state", "DOWN", 1},
		{"UNKNOWN state", "UNKNOWN", 2},
		{"BUSY state", "BUSY", 3},
		{"OUT OF SERVICE state", "OUT OF SERVICE", 4},
		{"OFS state abbreviation", "OFS", 4},
		{"TROFS state", "TROFS", 5},
		{"TRANSITION OUT OF SERVICE", "TRANSITION OUT OF SERVICE", 5},
		{"TROFS_DOWN state", "TROFS_DOWN", 8},

		// Case sensitivity tests
		{"UP lowercase", "up", 7},
		{"DOWN mixed case", "Down", 1},
		{"UNKNOWN titlecase", "Unknown", 2},

		// Whitespace handling
		{"UP with leading space", " UP", 7},
		{"DOWN with trailing space", "DOWN ", 1},
		{"UP with both spaces", " UP ", 7},
		{"BUSY with tabs", "\tBUSY\t", 3},

		// Invalid/unknown states (should return UNKNOWN = 2)
		{"Empty string", "", 2},
		{"Invalid state", "INVALID", 2},
		{"Numeric string", "123", 2},
		{"Special characters", "@#$", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNetscalerState(tt.input)
			if result != tt.expected {
				t.Errorf("parseNetscalerState(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParseNetscalerBinaryState tests the parseNetscalerBinaryState function
func TestParseNetscalerBinaryState(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float32
	}{
		// Valid enabled states
		{"ENABLED state", "ENABLED", 1},
		{"UP state", "UP", 1},

		// Valid disabled states
		{"DISABLED state", "DISABLED", 0},
		{"DOWN state", "DOWN", 0},

		// Case sensitivity tests
		{"enabled lowercase", "enabled", 1},
		{"DISABLED uppercase", "DISABLED", 0},
		{"Up mixed case", "Up", 1},

		// Whitespace handling
		{"ENABLED with spaces", " ENABLED ", 1},
		{"DOWN with tab", "\tDOWN", 0},

		// Invalid/unknown states (should return 0 = DISABLED)
		{"Empty string", "", 0},
		{"Invalid state", "INVALID", 0},
		{"Numeric string", "1", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNetscalerBinaryState(tt.input)
			if result != tt.expected {
				t.Errorf("parseNetscalerBinaryState(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParseNetscalerStateOfficialCodes verifies official Citrix ADC NITRO API codes
func TestParseNetscalerStateOfficialCodes(t *testing.T) {
	// Official Citrix ADC NITRO API state codes
	// Source: https://docs.netscaler.com/en-us/citrix-adc/current-release/load-balancing/load-balancing-vserver-service-states.html
	officialCodes := map[string]float32{
		"UP":              7,
		"DOWN":            1,
		"UNKNOWN":         2,
		"BUSY":            3,
		"OUT OF SERVICE":  4,
		"TROFS":           5,
		"TROFS_DOWN":      8,
	}

	for state, expectedCode := range officialCodes {
		t.Run("Official_code_"+state, func(t *testing.T) {
			result := parseNetscalerState(state)
			if result != expectedCode {
				t.Errorf("parseNetscalerState(%q) returned %v, but official Citrix ADC code is %v",
					state, result, expectedCode)
			}
		})
	}
}

// TestParseNetscalerBinaryStateOfficialCodes verifies official interface state codes
func TestParseNetscalerBinaryStateOfficialCodes(t *testing.T) {
	// Official Citrix ADC NITRO API interface state codes
	// Source: Citrix ADC NITRO API - interface state field
	officialCodes := map[string]float32{
		"ENABLED":  1,
		"DISABLED": 0,
	}

	for state, expectedCode := range officialCodes {
		t.Run("Official_code_"+state, func(t *testing.T) {
			result := parseNetscalerBinaryState(state)
			if result != expectedCode {
				t.Errorf("parseNetscalerBinaryState(%q) returned %v, but official Citrix ADC code is %v",
					state, result, expectedCode)
			}
		})
	}
}

// BenchmarkParseNetscalerState benchmarks the state parsing function
func BenchmarkParseNetscalerState(b *testing.B) {
	states := []string{"UP", "DOWN", "UNKNOWN", "BUSY", "OUT OF SERVICE", "TROFS", "TROFS_DOWN"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseNetscalerState(states[i%len(states)])
	}
}

// BenchmarkParseNetscalerBinaryState benchmarks the binary state parsing function
func BenchmarkParseNetscalerBinaryState(b *testing.B) {
	states := []string{"ENABLED", "DISABLED", "UP", "DOWN"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseNetscalerBinaryState(states[i%len(states)])
	}
}

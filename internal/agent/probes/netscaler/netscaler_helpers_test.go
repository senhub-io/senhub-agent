package netscaler

import (
	"math"
	"testing"
)

func TestGetFloat(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		key      string
		expected float64
	}{
		// Basic types
		{"float64 value", map[string]interface{}{"k": 42.5}, "k", 42.5},
		{"float32 value", map[string]interface{}{"k": float32(3.14)}, "k", float64(float32(3.14))},
		{"int value", map[string]interface{}{"k": 100}, "k", 100},
		{"int64 value", map[string]interface{}{"k": int64(999)}, "k", 999},
		{"uint value", map[string]interface{}{"k": uint(50)}, "k", 50},
		{"uint32 value", map[string]interface{}{"k": uint32(12345)}, "k", 12345},
		{"uint64 value", map[string]interface{}{"k": uint64(67890)}, "k", 67890},

		// String parsing
		{"string numeric", map[string]interface{}{"k": "123.45"}, "k", 123.45},
		{"string integer", map[string]interface{}{"k": "100"}, "k", 100},
		{"string zero", map[string]interface{}{"k": "0"}, "k", 0},

		// Sentinel value handling (4294967295 = max uint32, NITRO "no data")
		{"float64 sentinel", map[string]interface{}{"k": 4294967295.0}, "k", 0},
		{"float64 above sentinel", map[string]interface{}{"k": 4294967296.0}, "k", 0},
		{"float32 sentinel", map[string]interface{}{"k": float32(4294967295.0)}, "k", 0},
		{"uint32 sentinel", map[string]interface{}{"k": uint32(0xFFFFFFFF)}, "k", 0},
		{"uint64 sentinel", map[string]interface{}{"k": uint64(0xFFFFFFFF)}, "k", 0},
		{"string sentinel", map[string]interface{}{"k": "4294967295"}, "k", 0},

		// Edge cases
		{"missing key", map[string]interface{}{"other": 42}, "k", 0},
		{"nil map value", map[string]interface{}{"k": nil}, "k", 0},
		{"empty string", map[string]interface{}{"k": ""}, "k", 0},
		{"non-numeric string", map[string]interface{}{"k": "abc"}, "k", 0},
		{"zero float64", map[string]interface{}{"k": 0.0}, "k", 0},
		{"negative value", map[string]interface{}{"k": -5.0}, "k", -5.0},
		{"bool value (unsupported type)", map[string]interface{}{"k": true}, "k", 0},

		// Values just below sentinel
		{"just below sentinel", map[string]interface{}{"k": 4294967294.0}, "k", 4294967294.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFloat(tt.data, tt.key)
			if math.Abs(result-tt.expected) > 0.01 {
				t.Errorf("getFloat(%v, %q) = %v, expected %v", tt.data, tt.key, result, tt.expected)
			}
		})
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		key      string
		expected string
	}{
		{"string value", map[string]interface{}{"k": "hello"}, "k", "hello"},
		{"empty string", map[string]interface{}{"k": ""}, "k", ""},
		{"missing key", map[string]interface{}{"other": "val"}, "k", ""},
		{"non-string value", map[string]interface{}{"k": 42}, "k", ""},
		{"nil value", map[string]interface{}{"k": nil}, "k", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getString(tt.data, tt.key)
			if result != tt.expected {
				t.Errorf("getString(%v, %q) = %q, expected %q", tt.data, tt.key, result, tt.expected)
			}
		})
	}
}

func TestExtractCustomTags(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		wantCount int
	}{
		{
			"with custom tags",
			map[string]interface{}{
				"custom_tags": map[string]interface{}{
					"env":    "production",
					"region": "eu-west-1",
				},
			},
			2,
		},
		{
			"no custom tags",
			map[string]interface{}{},
			0,
		},
		{
			"nil custom tags",
			map[string]interface{}{"custom_tags": nil},
			0,
		},
		{
			"non-string tag values are skipped",
			map[string]interface{}{
				"custom_tags": map[string]interface{}{
					"valid": "yes",
					"number": 42,
				},
			},
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCustomTags(tt.config)
			if len(result) != tt.wantCount {
				t.Errorf("extractCustomTags() returned %d tags, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestIsBaseURLMatchingIP(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		ip      string
		want    bool
	}{
		{"IP match", "https://10.0.208.7", "10.0.208.7", true},
		{"IP mismatch", "https://10.0.208.7", "10.0.208.6", false},
		{"IP with port", "https://10.0.208.7:443", "10.0.208.7", true},
		{"Hostname does not resolve", "https://ns.example.com", "10.0.208.7", false},
		{"Empty IP", "https://10.0.208.7", "", false},
		{"Invalid URL", "://bad", "10.0.208.7", false},
		{"HTTP scheme", "http://10.0.208.7:8080", "10.0.208.7", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &netscalerProbe{activeURL: tt.baseURL}
			got := p.isBaseURLMatchingIP(tt.ip)
			if got != tt.want {
				t.Errorf("isBaseURLMatchingIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

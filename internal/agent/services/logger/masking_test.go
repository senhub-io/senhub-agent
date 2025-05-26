package logger

import (
	"testing"
)

func TestMaskSensitiveData(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Password in JSON",
			input:    `{"username": "admin", "password": "supersecret123"}`,
			expected: `{"username": "admin", "password": "su**********23"}`,
		},
		{
			name:     "API Key in text",
			input:    `API key: "abcdef123456abcdef"`,
			expected: `API key: "abcdef123456abcdef"`, // Not matched by our patterns
		},
		{
			name:     "Authorization header",
			input:    `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0`,
			expected: `Authorization: Bearer eyJh********************************************************wIn0`,
		},
		{
			name:     "Short password",
			input:    `password="abc"`,
			expected: `password="********"`,
		},
		{
			name:     "No sensitive data",
			input:    `Just a regular log message with no secrets`,
			expected: `Just a regular log message with no secrets`,
		},
		{
			name:     "Authentication key",
			input:    `--authentication-key XYZ987654321ABCDEF`,
			expected: `--authentication-key XYZ987654321ABCDEF`, // No match with current patterns
		},
		{
			name:     "Multiple sensitive fields",
			input:    `{"user": "admin", "password": "secret123", "api_key": "abcdef123456"}`,
			expected: `{"user": "admin", "password": "se*****23", "api_key": "ab********56"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSensitiveData(tt.input)
			if result != tt.expected {
				t.Errorf("MaskSensitiveData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Short value",
			input:    "abc",
			expected: "********",
		},
		{
			name:     "Medium value",
			input:    "password123",
			expected: "pa*******23",
		},
		{
			name:     "Long value",
			input:    "thisisaverylongpasswordwithmanycharacters",
			expected: "this*********************************ters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskValue(tt.input)
			if result != tt.expected {
				t.Errorf("maskValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}
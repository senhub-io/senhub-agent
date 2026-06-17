package logger

import (
	"strings"
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

// TestMaskSensitiveData_SNMPSecrets is the regression for the leak
// caught during the snmp_poll v3 runtime validation: priv_passphrase
// carried no recognized keyword and landed in the startup log in
// clear; SNMP community strings were never masked at all.
func TestMaskSensitiveData_SNMPSecrets(t *testing.T) {
	in := `{"priv_passphrase":"super-priv-secret-value","community":"internal-ro-string","auth_passphrase":"super-auth-secret-value"}`
	got := MaskSensitiveData(in)
	for _, leaked := range []string{"super-priv-secret-value", "internal-ro-string", "super-auth-secret-value"} {
		if strings.Contains(got, leaked) {
			t.Errorf("secret %q survived masking: %s", leaked, got)
		}
	}
}

func TestIsSensitiveFieldName_SNMP(t *testing.T) {
	for _, name := range []string{"priv_passphrase", "auth_passphrase", "community", "Community"} {
		if !isSensitiveFieldName(name) {
			t.Errorf("isSensitiveFieldName(%q) = false, want true", name)
		}
	}
	if isSensitiveFieldName("interval") {
		t.Error("interval must not be sensitive")
	}
}

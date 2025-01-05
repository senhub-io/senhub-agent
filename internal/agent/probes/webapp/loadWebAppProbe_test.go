package webapp

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewLoadWebAppProbe(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Valid Probe",
			config:  map[string]interface{}{"url": "http://example.com"},
			wantErr: false,
		},
		{
			name: "Valid Probe with timeout",
			config: map[string]interface{}{
				"url":     "http://example.com",
				"timeout": "10s",
			},
			wantErr: false,
		},
		{
			name:    "Invalid Probe: Missing URL",
			config:  map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "Invalid Probe: Timeout is not a valid duration",
			config: map[string]interface{}{
				"url":     "http://example.com",
				"timeout": "abc",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLoadWebAppProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLoadWebAppProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

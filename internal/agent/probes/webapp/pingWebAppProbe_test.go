package webapp

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewPingWebAppProbe(t *testing.T) {
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
			name:    "Invalid Probe: Missing URL",
			config:  map[string]interface{}{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPingWebAppProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPingWebAppProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

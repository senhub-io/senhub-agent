package host

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewLogicalDiskProbe(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Valid Probe",
			config:  map[string]interface{}{},
			wantErr: false, // Darwin is now supported
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLogicalDiskProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogicalDiskProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Darwin is now supported, no special error handling needed
		})
	}
}

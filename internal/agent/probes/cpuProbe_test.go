package probes

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewCpuProbe(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Valid Probe",
			config:  map[string]interface{}{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCpuProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCpuProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

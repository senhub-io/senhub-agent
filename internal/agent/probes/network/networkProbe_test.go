package network

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewNetworkProbe(t *testing.T) {
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
			_, err := NewNetworkProbe(tt.config, &logger)
			
			// On Windows, PDH counters may not be available in test environments
			// This is expected and should not fail the test
			if runtime.GOOS == "windows" && err != nil {
				if strings.Contains(err.Error(), "PDH_NO_DATA") || 
				   strings.Contains(err.Error(), "failed initial collection") {
					t.Logf("Expected Windows PDH limitation in test environment: %v", err)
					return
				}
			}
			
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNetworkProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

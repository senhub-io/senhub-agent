package gateway

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewPingGatewayProbe(t *testing.T) {
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
			_, err := NewPingGatewayProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPingGatewayProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

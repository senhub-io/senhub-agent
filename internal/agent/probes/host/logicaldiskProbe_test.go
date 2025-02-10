package host

import (
	"os"
	"runtime"
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
			wantErr: runtime.GOOS == "darwin", // On s'attend à une erreur sur Darwin
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLogicalDiskProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogicalDiskProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Si on est sur Darwin, vérifions que c'est bien l'erreur attendue
			if runtime.GOOS == "darwin" && err != nil {
				expectedError := "unsupported operating system: darwin"
				if err.Error() != expectedError {
					t.Errorf("Expected error message '%s', got '%s'", expectedError, err.Error())
				}
			}
		})
	}
}

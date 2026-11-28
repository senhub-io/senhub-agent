package agent

import (
	"testing"

	"senhub-agent.go/internal/agent/services/auto_update"
)

func TestStartupVersionCheckWanted(t *testing.T) {
	tests := []struct {
		name        string
		fileEnabled bool
		directive   *auto_update.Directive
		want        bool
	}{
		{
			name:        "file disabled, no directive",
			fileEnabled: false,
			directive:   nil,
			want:        false,
		},
		{
			name:        "file enabled, no directive",
			fileEnabled: true,
			directive:   nil,
			want:        true,
		},
		{
			name:        "directive auto overrides disabled file",
			fileEnabled: false,
			directive:   &auto_update.Directive{Auto: true},
			want:        true,
		},
		{
			name:        "directive non-auto overrides enabled file",
			fileEnabled: true,
			directive:   &auto_update.Directive{Auto: false},
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startupVersionCheckWanted(tt.fileEnabled, tt.directive)
			if got != tt.want {
				t.Errorf("startupVersionCheckWanted(%v, %v) = %v, want %v",
					tt.fileEnabled, tt.directive, got, tt.want)
			}
		})
	}
}

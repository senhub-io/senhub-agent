package http

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestWithTargetDown(t *testing.T) {
	original := []PRTGChannel{{Channel: "first"}, {Channel: "second"}}
	state := agentstate.ProbeRunState{Running: true, TargetDown: true, TargetError: "timeout"}

	tests := []struct {
		name           string
		res            PRTGResult
		state          agentstate.ProbeRunState
		known          bool
		enabled        bool
		wantText       string
		wantWarnings   []int
		wantUnmodified bool
	}{
		{
			name:           "disabled",
			res:            PRTGResult{Result: original},
			state:          state,
			known:          true,
			enabled:        false,
			wantText:       "",
			wantWarnings:   []int{0, 0},
			wantUnmodified: true,
		},
		{
			name:           "unknown probe",
			res:            PRTGResult{Result: original},
			state:          state,
			known:          false,
			enabled:        true,
			wantText:       "",
			wantWarnings:   []int{0, 0},
			wantUnmodified: true,
		},
		{
			name:           "target down with cause",
			res:            PRTGResult{Result: original},
			state:          state,
			known:          true,
			enabled:        true,
			wantText:       "Target down: timeout",
			wantWarnings:   []int{1, 0},
			wantUnmodified: true,
		},
		{
			name:           "target down without channels",
			res:            PRTGResult{},
			state:          state,
			known:          true,
			enabled:        true,
			wantText:       "",
			wantWarnings:   nil,
			wantUnmodified: true,
		},
		{
			name:           "running without target down",
			res:            PRTGResult{Result: original},
			state:          agentstate.ProbeRunState{Running: true, TargetDown: false, TargetError: "timeout"},
			known:          true,
			enabled:        true,
			wantText:       "",
			wantWarnings:   []int{0, 0},
			wantUnmodified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := make([]PRTGChannel, len(tt.res.Result))
			copy(before, tt.res.Result)

			got := withTargetDown(tt.res, tt.state, tt.known, tt.enabled)

			if got.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
			}
			for i, want := range tt.wantWarnings {
				if got.Result[i].Warning != want {
					t.Errorf("channel %d Warning = %d, want %d", i, got.Result[i].Warning, want)
				}
			}
			if tt.wantUnmodified {
				for i := range before {
					if tt.res.Result[i] != before[i] {
						t.Errorf("input channel %d modified: %+v, want %+v", i, tt.res.Result[i], before[i])
					}
				}
			}
		})
	}
}

func TestPRTGTargetDownWarningDefaultsOn(t *testing.T) {
	if defaultPRTGTargetDownWarning != true {
		t.Errorf("defaultPRTGTargetDownWarning = %v, want true", defaultPRTGTargetDownWarning)
	}
}

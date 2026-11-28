package http

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestWithNagiosTargetDown(t *testing.T) {
	down := agentstate.ProbeRunState{Running: true, TargetDown: true, TargetError: "timeout"}
	noError := agentstate.ProbeRunState{Running: true, TargetDown: true}
	up := agentstate.ProbeRunState{Running: true, TargetDown: false}

	tests := []struct {
		name         string
		resp         NagiosResponse
		probeName    string
		state        agentstate.ProbeRunState
		known        bool
		wantStatus   int
		wantText     string
		wantMessage  string
		wantPerfData string
	}{
		{
			name:         "ok with target down",
			resp:         NagiosResponse{Status: 0, StatusText: "OK", Message: "OK", PerfData: "'time'=0.1s"},
			probeName:    "web",
			state:        down,
			known:        true,
			wantStatus:   1,
			wantText:     "WARNING",
			wantMessage:  "Probe web target down: timeout",
			wantPerfData: "'time'=0.1s",
		},
		{
			name:        "critical with target down",
			resp:        NagiosResponse{Status: 2, StatusText: "CRITICAL", Message: "CRITICAL"},
			probeName:   "web",
			state:       down,
			known:       true,
			wantStatus:  2,
			wantText:    "CRITICAL",
			wantMessage: "CRITICAL (last check: timeout)",
		},
		{
			name:        "ok without target down",
			resp:        NagiosResponse{Status: 0, StatusText: "OK", Message: "OK"},
			probeName:   "web",
			state:       up,
			known:       true,
			wantStatus:  0,
			wantText:    "OK",
			wantMessage: "OK",
		},
		{
			name:        "unknown probe",
			resp:        NagiosResponse{Status: 0, StatusText: "OK", Message: "OK"},
			probeName:   "web",
			state:       down,
			known:       false,
			wantStatus:  0,
			wantText:    "OK",
			wantMessage: "OK",
		},
		{
			name:        "empty target error",
			resp:        NagiosResponse{Status: 0, StatusText: "OK", Message: "OK"},
			probeName:   "web",
			state:       noError,
			known:       true,
			wantStatus:  1,
			wantText:    "WARNING",
			wantMessage: "Probe web target down: check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withNagiosTargetDown(tt.resp, tt.probeName, tt.state, tt.known)

			if got.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", got.Status, tt.wantStatus)
			}
			if got.StatusText != tt.wantText {
				t.Errorf("StatusText = %q, want %q", got.StatusText, tt.wantText)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if got.PerfData != tt.wantPerfData {
				t.Errorf("PerfData = %q, want %q", got.PerfData, tt.wantPerfData)
			}
		})
	}
}

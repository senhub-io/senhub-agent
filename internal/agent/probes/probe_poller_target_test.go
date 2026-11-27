package probes

import (
	"testing"

	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestCheckOutcome(t *testing.T) {
	tests := []struct {
		name     string
		data     []datapoint.DataPoint
		seen     bool
		down     bool
		reason   string
		wantSeen bool
		wantDown bool
	}{
		{
			name:     "no check points",
			data:     []datapoint.DataPoint{{Name: "other.metric", Value: 1}},
			wantSeen: false,
			wantDown: false,
		},
		{
			name:     "single ok",
			data:     []datapoint.DataPoint{{Name: "senhub.probe.check.result", Value: 0}},
			wantSeen: true,
			wantDown: false,
		},
		{
			name:     "single failure",
			data:     []datapoint.DataPoint{{Name: "senhub.probe.check.result", Value: 1}},
			wantSeen: true,
			wantDown: true,
			reason:   "target_down",
		},
		{
			name: "two failures",
			data: []datapoint.DataPoint{
				{Name: "senhub.probe.check.result", Value: 0},
				{Name: "senhub.probe.check.result", Value: 3},
				{Name: "senhub.probe.check.result", Value: 1},
			},
			wantSeen: true,
			wantDown: true,
			reason:   "timeout (2 checks failing)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen, down, reason := checkOutcome(tt.data)
			if seen != tt.wantSeen {
				t.Errorf("checkOutcome() seen = %v, want %v", seen, tt.wantSeen)
			}
			if down != tt.wantDown {
				t.Errorf("checkOutcome() down = %v, want %v", down, tt.wantDown)
			}
			if reason != tt.reason {
				t.Errorf("checkOutcome() reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

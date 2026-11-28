package statuschan

import (
	"testing"

	"senhub-agent.go/internal/agent/services/status"
)

func TestHealthForTargetDown(t *testing.T) {
	tests := []struct {
		name       string
		total      int
		failed     int
		targetDown int
		wantStatus string
		wantMsg    string
	}{
		{
			name:       "no failures keeps base",
			total:      4,
			failed:     0,
			targetDown: 0,
			wantStatus: "healthy",
		},
		{
			name:       "target down degrades",
			total:      4,
			failed:     0,
			targetDown: 1,
			wantStatus: "degraded",
			wantMsg:    "1 probe(s) report their target down",
		},
		{
			name:       "single probe with its target down is degraded, not unhealthy",
			total:      1,
			failed:     0,
			targetDown: 1,
			wantStatus: "degraded",
			wantMsg:    "1 probe(s) report their target down",
		},
		{
			name:       "failed wins over target down",
			total:      4,
			failed:     1,
			targetDown: 1,
			wantStatus: "degraded",
			wantMsg:    "1 probe(s) have errors",
		},
		{
			name:       "many failures unhealthy",
			total:      4,
			failed:     2,
			targetDown: 0,
			wantStatus: "unhealthy",
			wantMsg:    "2 of 4 probes have errors",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := status.HealthInfo{Status: "healthy"}
			got := healthFor(base, tt.total, tt.failed, tt.targetDown)
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

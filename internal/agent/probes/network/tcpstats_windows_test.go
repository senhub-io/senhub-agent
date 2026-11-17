//go:build windows

package network

import (
	"testing"
)

// TestWindowsTCPStateTable verifies the mapping from Windows TCP state strings
// to their corresponding numeric codes and validity flags.
func TestWindowsTCPStateTable(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		wantCode uint8
		wantOK   bool
	}{
		{"ESTABLISHED", "ESTABLISHED", 1, true},
		{"SYN_SENT", "SYN_SENT", 2, true},
		{"SYN_RECEIVED", "SYN_RECEIVED", 3, true},
		{"FIN_WAIT_1", "FIN_WAIT_1", 4, true},
		{"FIN_WAIT_2", "FIN_WAIT_2", 5, true},
		{"TIME_WAIT", "TIME_WAIT", 6, true},
		{"CLOSED", "CLOSED", 7, true},
		{"CLOSE_WAIT", "CLOSE_WAIT", 8, true},
		{"LAST_ACK", "LAST_ACK", 9, true},
		{"LISTEN", "LISTEN", 10, true},
		{"CLOSING", "CLOSING", 11, true},
		{"DELETE", "DELETE", 0, false},
		{"EmptyString", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCode, gotOK := windowsTCPState(tt.status)
			if gotCode != tt.wantCode || gotOK != tt.wantOK {
				t.Errorf("windowsTCPState(%q) = (%d, %t), want (%d, %t)",
					tt.status, gotCode, gotOK, tt.wantCode, tt.wantOK)
			}
		})
	}
}

// TestReadTCPStatsWindowsLive checks real TCP statistics from the system.
// The test is skipped if reading fails, as it may not be available in all
// environments (e.g., restricted permissions).
func TestReadTCPStatsWindowsLive(t *testing.T) {
	stats, err := readTCPStatsWindows()
	if err != nil {
		t.Skipf("skipping live test: cannot read TCP stats: %v", err)
	}

	if stats.HasListenOverflows {
		t.Error("HasListenOverflows should be false for a healthy system")
	}
}

// TestWindowsTCPConnsLive verifies that we can retrieve active TCP connections
// and that each connection's state is within the valid Windows TCP state range.
// The test is skipped if enumeration fails.
func TestWindowsTCPConnsLive(t *testing.T) {
	conns, err := windowsTCPConns()
	if err != nil {
		t.Skipf("skipping live test: cannot enumerate TCP connections: %v", err)
	}

	for i, conn := range conns {
		if conn.State < 1 || conn.State > 11 {
			t.Errorf("connection %d has invalid state %d, want between 1 and 11", i, conn.State)
		}
	}
}

package hostservice

import "testing"

func TestOnlyARunningServiceCountsAsRunning(t *testing.T) {
	for state, want := range map[string]float64{"active": 1, "reloading": 1, "inactive": 0, "failed": 0, "activating": 0, "deactivating": 0, "maintenance": 0} {
		if got := RunningSystemd(state); got != want {
			t.Errorf("RunningSystemd(%q) = %v, want %v", state, got, want)
		}
	}
	for state, want := range map[int]float64{4: 1, 1: 0, 2: 0, 3: 0, 5: 0, 6: 0, 7: 0} {
		if got := RunningWindows(state); got != want {
			t.Errorf("RunningWindows(%d) = %v, want %v", state, got, want)
		}
	}
}

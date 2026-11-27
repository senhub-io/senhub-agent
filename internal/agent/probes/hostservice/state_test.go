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

func TestStatusSystemd(t *testing.T) {
	for _, tc := range []struct {
		activeState string
		want        string
	}{
		{"active", "running"},
		{"reloading", "running"},
		{"refreshing", "running"},
		{"inactive", "stopped"},
		{"maintenance", "stopped"},
		{"activating", "starting"},
		{"deactivating", "stopping"},
		{"failed", "failed"},
		{"", "unknown"},
		{"maintenance-mode", "unknown"},
	} {
		if got := StatusSystemd(tc.activeState); got != tc.want {
			t.Errorf("StatusSystemd(%q) = %q, want %q", tc.activeState, got, tc.want)
		}
	}
}

func TestStatusWindows(t *testing.T) {
	for _, tc := range []struct {
		scmState int
		want     string
	}{
		{1, "stopped"},
		{2, "starting"},
		{5, "starting"},
		{3, "stopping"},
		{6, "stopping"},
		{4, "running"},
		{7, "paused"},
		{0, "unknown"},
		{8, "unknown"},
	} {
		if got := StatusWindows(tc.scmState); got != tc.want {
			t.Errorf("StatusWindows(%d) = %q, want %q", tc.scmState, got, tc.want)
		}
	}
}

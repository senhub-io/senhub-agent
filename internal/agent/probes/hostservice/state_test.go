package hostservice

import "testing"

func TestTheSameSituationGivesTheSameValue(t *testing.T) {
	pairs := []struct {
		systemd string
		windows int
		want    int
	}{
		{"active", 4, Running},
		{"inactive", 1, Stopped},
		{"activating", 2, Starting},
		{"deactivating", 3, Stopping},
	}
	for _, p := range pairs {
		if got := FromSystemd(p.systemd); got != p.want {
			t.Errorf("FromSystemd(%q) = %d, want %d", p.systemd, got, p.want)
		}
		if got := FromWindows(p.windows); got != p.want {
			t.Errorf("FromWindows(%d) = %d, want %d", p.windows, got, p.want)
		}
	}
	if FromSystemd("failed") != Failed || FromWindows(7) != Paused || FromSystemd("maintenance") != Other {
		t.Error("failed, paused and unknown states keep their own values")
	}
}

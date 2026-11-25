package status

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestFormatUpdate(t *testing.T) {
	f := NewCLIFormatter()

	if got := f.formatUpdate(nil); got != "" {
		t.Errorf("nil update rendered %q, want empty", got)
	}

	got := f.formatUpdate(&UpdateInfo{Enabled: true, Target: "latest", Source: agentstate.UpdateSourceLocal})
	want := "Auto-update: enabled, target latest (local file)\n"
	if got != want {
		t.Errorf("local enabled:\ngot  %q\nwant %q", got, want)
	}

	got = f.formatUpdate(&UpdateInfo{Enabled: false, Source: agentstate.UpdateSourceServer, Target: "0.7.2"})
	want = "Auto-update: disabled (set by a server)\n"
	if got != want {
		t.Errorf("server disabled:\ngot  %q\nwant %q", got, want)
	}

	got = f.formatUpdate(&UpdateInfo{Enabled: true, IncludeBeta: true, Target: "latest", Source: agentstate.UpdateSourceServer, Paused: true})
	if !strings.HasPrefix(got, "Auto-update: enabled, target latest, beta channel (set by a server)\n") {
		t.Errorf("beta paused:\ngot %q", got)
	}
	if !strings.Contains(got, "  Paused by an extension: the periodic check does not run until it is resumed\n") {
		t.Errorf("paused line missing: %q", got)
	}
}

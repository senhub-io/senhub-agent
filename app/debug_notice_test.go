package app

import (
	"strings"
	"testing"
)

// A degraded view that looks like an answer is worse than no view: the
// operator reads uptime 0s as the agent's, on a host where the agent is
// fine. Pins #851.
func TestDaemonUnreachableNoticeSaysWhy(t *testing.T) {
	if got := daemonUnreachableNotice("", ""); got != "" {
		t.Errorf("a daemon that answered needs no notice, got %q", got)
	}

	notice := daemonUnreachableNotice("the agent key could not be read from /etc/agent.yaml (agent key not found in config file)", "")
	if !strings.Contains(notice, "could not be asked") || !strings.Contains(notice, "agent key could not be read") {
		t.Errorf("the notice must name the cause, got %q", notice)
	}
	if !strings.Contains(notice, "not the service's state") {
		t.Errorf("the notice must label what follows as the local view, got %q", notice)
	}

	// An unresolved key is what makes the port unreachable, so it is the
	// cause worth printing.
	both := daemonUnreachableNotice("key unresolved", "port 8080 refused")
	if !strings.Contains(both, "key unresolved") || strings.Contains(both, "port 8080") {
		t.Errorf("the key problem is the one to report, got %q", both)
	}
}

package otlp

import (
	"strings"
	"testing"
)

// TestDependsOnBlindMessageDoesNotTellRootToBecomeRoot pins the shape a client
// reported: the warning advised running as root while its own field said
// running_as=root. Reproduced on a host — two containers sharing a network
// namespace, agent as root, every socket unattributable — and no privilege
// changes it: the socket table belongs to the shared network namespace, the
// processes belong to their own, so the owners are not merely unreadable, they
// are absent.
func TestDependsOnBlindMessageDoesNotTellRootToBecomeRoot(t *testing.T) {
	asRoot := dependsOnBlindMessage(0)

	if strings.Contains(asRoot, "Run the agent as root") {
		t.Errorf("the message tells root to run as root:\n%s", asRoot)
	}
	if !strings.Contains(asRoot, "privileges are not the cause") {
		t.Errorf("the message must say privileges are not the cause:\n%s", asRoot)
	}
	if !strings.Contains(asRoot, "PID namespace") {
		t.Errorf("the message must name what to change:\n%s", asRoot)
	}
}

// TestDependsOnBlindMessageStillAdvisesRootWhenItWouldHelp keeps the other
// half: an unprivileged daemon genuinely cannot read another user's
// /proc/<pid>/fd, and that advice is the right one (#808).
func TestDependsOnBlindMessageStillAdvisesRootWhenItWouldHelp(t *testing.T) {
	asUser := dependsOnBlindMessage(1000)

	if !strings.Contains(asUser, "Run the agent as root") {
		t.Errorf("an unprivileged agent must still be told the privilege answer:\n%s", asUser)
	}
	if strings.Contains(asUser, "privileges are not the cause") {
		t.Errorf("the root-specific explanation leaked into the unprivileged message:\n%s", asUser)
	}
}

// TestDependsOnBlindMessageAlwaysOffersTheWayOut: whichever branch, the
// operator must learn that the rail can be turned off.
func TestDependsOnBlindMessageAlwaysOffersTheWayOut(t *testing.T) {
	for _, euid := range []int{0, 1000} {
		msg := dependsOnBlindMessage(euid)
		if !strings.Contains(msg, "entities.depends_on_enabled") {
			t.Errorf("euid %d: the message must name the switch to turn off:\n%s", euid, msg)
		}
		if !strings.Contains(msg, "/proc/<pid>/fd") {
			t.Errorf("euid %d: the message must say what it reads:\n%s", euid, msg)
		}
	}
}

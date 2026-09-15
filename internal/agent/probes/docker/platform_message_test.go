package docker

import (
	"strings"
	"testing"
)

// TestNoUnifiedCgroupMessageNamesThePlatform pins what an operator is
// told when the engine cannot be reached and there is no unified cgroup
// tree to fall back on.
//
// On Windows the probe used to answer "this host uses cgroup v1", on a
// platform that has no cgroups at all — a diagnosis pointing at
// something that cannot exist there. #801 is what let the probe reach
// this point on Windows in the first place, so the wrong message
// shipped with the feature that made it reachable.
func TestNoUnifiedCgroupMessageNamesThePlatform(t *testing.T) {
	linux := noUnifiedCgroupMessage("linux")
	if !strings.Contains(linux, "cgroup v1") {
		t.Errorf("the Linux message must name cgroup v1, the thing that is actually there: %q", linux)
	}

	for goos, expected := range map[string]string{"windows": "named pipe", "darwin": "socket"} {
		msg := noUnifiedCgroupMessage(goos)
		if strings.Contains(msg, "cgroup") {
			t.Errorf("%s: the message names cgroups, which this platform does not have: %q", goos, msg)
		}
		if !strings.Contains(msg, "unreachable") {
			t.Errorf("%s: the message must still say what went wrong: %q", goos, msg)
		}
		if !strings.Contains(msg, expected) {
			t.Errorf("%s: the message must name what this platform actually uses (%s): %q", goos, expected, msg)
		}
	}

	if noUnifiedCgroupMessage("windows") == linux {
		t.Error("both platforms get the same message; the branch does nothing")
	}
}

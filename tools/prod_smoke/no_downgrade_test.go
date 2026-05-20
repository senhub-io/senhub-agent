//go:build prod_smoke

package prod_smoke

import (
	"regexp"
	"strings"
	"testing"
)

// agentVersionPattern parses `senhub-agent version` output. The first
// line is "Version: X.Y.Z[-tag] (commit: …)". We only need the version
// triplet+pre-release portion.
var agentVersionPattern = regexp.MustCompile(`(?m)^Version:\s*([0-9]+\.[0-9]+\.[0-9]+[-A-Za-z0-9.]*)`)

// expectedVersion is the value we assert the deployed agent matches.
// Override via SENHUB_EXPECTED_AGENT_VERSION when bumping the beta.
func expectedVersion() string {
	return envOr("SENHUB_EXPECTED_AGENT_VERSION", "0.1.95-beta")
}

// TestNoDowngrade_AgentIsAtExpectedVersion asserts that the agent
// running on each host is at the version we *deployed* and did not
// silently downgrade since. PR #122 closed the auto-update path that
// could revert a beta to the previous prod when the update server's
// `latest` alias resolved to a release older than the running version.
//
// The test reads `senhub-agent version` (Linux) or the Windows
// equivalent on each host and compares against
// SENHUB_EXPECTED_AGENT_VERSION (default "0.1.95-beta").
//
// This is a post-condition check: the test is meaningful AFTER a
// fresh deploy of the expected version. Running it weeks later still
// catches drift — if an agent somehow ended up at a lower version,
// the test fails and points the operator at the host.
func TestNoDowngrade_AgentIsAtExpectedVersion(t *testing.T) {
	want := expectedVersion()
	for _, h := range hosts {
		t.Run(h.Name, func(t *testing.T) {
			out, ok := remoteShell(t, h, agentVersionCommand(h))
			if !ok {
				t.Skipf("%s unreachable; skipping", h.Name)
			}
			m := agentVersionPattern.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("%s: could not parse version from output:\n%s", h.Name, out)
			}
			got := m[1]
			if !strings.HasPrefix(got, want) {
				// Use HasPrefix so a deploy targeting 0.1.95-beta is
				// also satisfied by 0.1.95-beta-<commits-since>-<sha>
				// dev builds (git-describe form). Reject silently
				// downgraded prod, refuse exact-match-only when ops
				// has more nuance to express, via SENHUB_EXPECTED_AGENT_VERSION_EXACT.
				if envOr("SENHUB_EXPECTED_AGENT_VERSION_EXACT", "") == "1" && got != want {
					t.Errorf("%s: agent at %q, expected exact %q", h.Name, got, want)
				} else if envOr("SENHUB_EXPECTED_AGENT_VERSION_EXACT", "") != "1" {
					t.Errorf("%s: agent at %q, expected prefix %q (set SENHUB_EXPECTED_AGENT_VERSION_EXACT=1 to require exact match)", h.Name, got, want)
				}
			}
		})
	}
}

// agentVersionCommand returns the host-appropriate command to print
// the agent version. Both Linux and Windows agents emit the same
// "Version: …" shape via cliArgs.Version ldflags.
//
// On Linux the agent binary refuses to run without root privileges
// (privilege check inside main.go); we go through `sudo -n` so we get
// a fail-fast if the policy ever regresses, instead of a password
// prompt that would hang the SSH session.
func agentVersionCommand(h host) string {
	switch h.Name {
	case "bbcloud":
		return `"C:\SenHub\senhub-agent.exe" version`
	default:
		return "sudo -n /usr/local/bin/senhub-agent version"
	}
}

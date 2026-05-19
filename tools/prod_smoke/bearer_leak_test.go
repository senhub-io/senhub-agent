//go:build prod_smoke

package prod_smoke

import (
	"regexp"
	"strings"
	"testing"
)

// bearerCredentialPattern catches the shape of OTLP bearer tokens that
// the agent's OTel collector returns in auth-failure error messages.
// Two formats observed in the wild:
//   - hex64: `Bearer 0577b821b8af...adef20e0adb447efda9df498c909077d80a4`
//   - UUID + suffix: `Bearer 4947726a-3e85-4706-8be1-134d83e2a29f-03e2c318`
//
// We treat anything matching `Bearer ` + 20+ hex/dash chars as a leak.
// Shorter tokens or non-credential prose ("bearer-token-auth extension")
// don't match the trailing length constraint.
var bearerCredentialPattern = regexp.MustCompile(`(?i)Bearer [0-9a-f][-0-9a-f]{19,}`)

// TestBearerLeak_NoBearerInLog asserts that the redaction shipped by
// PR #120 is effective in production: the agent log file on each
// reachable host MUST NOT contain a bearer-shaped token.
//
// The test reads the agent log on each host, runs the regex over the
// content, and fails with the line numbers / matches if any survive.
// It does NOT trigger an auth failure first — the deploy itself
// exercises that path on first restart when the collector handshake
// fires; this test verifies the LOG path, not the auth path.
func TestBearerLeak_NoBearerInLog(t *testing.T) {
	for _, h := range hosts {
		t.Run(h.Name, func(t *testing.T) {
			out, ok := remoteShell(t, h, logTailCommand(h))
			if !ok {
				t.Skipf("%s unreachable; skipping", h.Name)
			}
			// Scope the check to the slice after the most recent
			// service restart. The 0.1.95-beta agent's redaction
			// only applies to entries it writes itself — historical
			// entries from a previous (vulnerable) version stay on
			// disk and shouldn't fail the test.
			out = sinceLastRestart(out)
			matches := bearerCredentialPattern.FindAllString(out, -1)
			if len(matches) > 0 {
				// Don't print the matches themselves — defeats the
				// whole point of the test. Just count + the first
				// 4 chars of each as a fingerprint.
				fingerprints := make([]string, 0, len(matches))
				for _, m := range matches {
					if i := strings.Index(m, " "); i != -1 && len(m) > i+4 {
						fingerprints = append(fingerprints, m[:i+5]+"…")
					} else {
						fingerprints = append(fingerprints, "Bearer …")
					}
				}
				t.Errorf("found %d bearer-shaped token(s) in %s agent log; fingerprints (first 4 chars each): %v",
					len(matches), h.Name, fingerprints)
			}
		})
	}
}

// logTailCommand returns the host-appropriate command to dump the
// recent agent log content. Linux: journalctl (the user has read
// access without sudo via the systemd-journal group). Windows:
// PowerShell on the known path. We pull a generous tail (last 5000
// lines) so the test covers a full restart + several push cycles.
//
// Importantly: we never use `sudo` here. The test SSH session runs
// in BatchMode (no TTY), so if sudo prompted for a password the
// session would hang until the SSH command timeout — easier to keep
// the command itself non-interactive.
func logTailCommand(h host) string {
	switch h.Name {
	case "bbcloud":
		// The agent log on Windows is written as a single physical
		// line containing concatenated JSON blobs (zerolog default,
		// no newlines). `Get-Content -Tail N` would scan backwards
		// looking for newlines that don't exist — that's an infinite
		// loop in practice. `-Raw` reads the whole file as one
		// string in one shot; the file is ~20 MB max in our window,
		// trivial to slurp once per test.
		return `powershell -Command "Get-Content -Raw C:\ProgramData\SenHub\logs\senhubagent.log"`
	default:
		return "journalctl -u senhub-agent --no-pager -n 5000"
	}
}

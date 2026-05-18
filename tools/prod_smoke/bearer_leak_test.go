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
// recent agent log content. Linux: tail. Windows: PowerShell on the
// known path. We pull a generous tail (last 5000 lines) so the test
// covers a full restart + several push cycles.
func logTailCommand(h host) string {
	switch h.Name {
	case "bbcloud":
		// PowerShell quoting via SSH: single-arg command — wrap in
		// `powershell -Command "..."` and use Get-Content -Tail.
		return `powershell -Command "Get-Content -Tail 5000 C:\ProgramData\SenHub\logs\senhubagent.log"`
	default:
		// Linux / systemd: journalctl preferred over file-tail because
		// log rotation may have moved the on-disk file.
		return "sudo journalctl -u senhub-agent --no-pager -n 5000 2>/dev/null || tail -n 5000 /var/log/senhub-agent/senhubagent.log"
	}
}

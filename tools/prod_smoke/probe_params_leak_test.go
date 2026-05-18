//go:build prod_smoke

package prod_smoke

import (
	"regexp"
	"testing"
)

// probeStartLogPattern catches "Starting new probe" entries from the
// sensor. The redaction shipped by PR #121 replaces sensitive values
// with "***", so a clean log has key-quoted-value pairs only for the
// asterisk pattern when the key name is sensitive.
var probeStartLogPattern = regexp.MustCompile(`"message":"Starting new probe"[^}]*"probe_params":\{[^}]*\}`)

// secretKeyWithPlainValuePattern looks for any "probe_params" entry
// where a sensitive key name (key|token|password|secret|user|login|
// email|credential — case-insensitive) is followed by a non-"***"
// value. If PR #121's redaction is effective, this returns no matches.
var secretKeyWithPlainValuePattern = regexp.MustCompile(`(?i)"(api_?key|auth_?key|password|token|secret|user(name)?|login|email|credential)[^"]*":"(?!\*\*\*")[^"]+"`)

// TestProbeParamsLeak_NoUserInLog asserts that the redaction shipped
// by PR #121 covers identifier-style fields the legacy log dumped
// verbatim. The failing pattern on bbcloud was the IBM i probe's
// "user":"matnosson" — an authentic auth identifier on pub400.com.
//
// We pull recent agent logs from each host, locate every
// "Starting new probe" entry, and assert that no occurrence carries
// a sensitive key name with a non-redacted value. False negatives
// here would mean either the redaction wasn't applied at the call
// site OR a new probe shape emits a sensitive value we don't yet
// pattern-match.
func TestProbeParamsLeak_NoUserInLog(t *testing.T) {
	for _, h := range hosts {
		t.Run(h.Name, func(t *testing.T) {
			out, ok := remoteShell(t, h, logTailCommand(h))
			if !ok {
				t.Skipf("%s unreachable; skipping", h.Name)
			}

			// First narrow to probe-start lines — anything else may
			// legitimately mention user identifiers (e.g. license
			// validation log lines reference subjects).
			probeStartEntries := probeStartLogPattern.FindAllString(out, -1)
			if len(probeStartEntries) == 0 {
				// No probe-start entries observed yet — log file may
				// be truncated since the last service restart.
				// Don't fail; just note.
				t.Logf("%s: no 'Starting new probe' entries in recent log window; nothing to check", h.Name)
				return
			}

			var leaks []string
			for _, entry := range probeStartEntries {
				for _, m := range secretKeyWithPlainValuePattern.FindAllString(entry, -1) {
					// Fingerprint the matched key only, not the value.
					if i := indexOf(m, `":"`); i != -1 {
						leaks = append(leaks, m[:i+3]+"…")
					} else {
						leaks = append(leaks, "<match>")
					}
				}
			}
			if len(leaks) > 0 {
				t.Errorf("%s: %d probe_params leak(s) in recent agent log (fingerprints of leaked KEY names — values redacted): %v",
					h.Name, len(leaks), leaks)
			}
		})
	}
}

// indexOf is a stdlib-free strings.Index, kept tiny so tests don't
// pull strings just for this.
func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

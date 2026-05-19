//go:build prod_smoke

package prod_smoke

import "strings"

// sinceLastRestart returns the portion of log content that follows
// the most recent agent restart, identified by the first
// "Initializing agent" log line. Pre-restart entries belong to a
// historical agent build and are excluded from regression checks —
// the tests target the *currently deployed* version's behaviour, not
// whatever the previous version did before redeploy.
//
// When no restart marker is found the full content is returned. That
// preserves the test's value on hosts where the marker phrase
// changes (forward compatibility) and on fresh installs whose first
// log entry is the only restart.
//
// The marker is the literal phrase the agent's main.go emits on
// startup ("Initializing agent in offline mode"). If a future
// refactor rewords this line, update the constant here and add a
// regression test to keep them in sync.
const restartMarker = `"Initializing agent in offline mode"`

func sinceLastRestart(log string) string {
	idx := strings.LastIndex(log, restartMarker)
	if idx < 0 {
		return log
	}
	return log[idx:]
}

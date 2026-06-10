package app

import "strings"

// migrateImagePathValue rewrites a legacy Windows service ImagePath
// that lacks the `run` subcommand (#309). Services installed before
// 0.2.x registered:
//
//	"C:\SenHub\senhub-agent.exe" --config-path C:\SenHub\agent.yaml
//
// The 0.2.x CLI requires a subcommand: launched without one the binary
// prints usage and exits, which the SCM reports as "the service did
// not respond in a timely fashion" — an in-place binary upgrade leaves
// the service unable to start. The migrated form inserts `run` right
// after the executable:
//
//	"C:\SenHub\senhub-agent.exe" run --config-path C:\SenHub\agent.yaml
//
// Returns the (possibly rewritten) value and whether a rewrite was
// needed. Pure string logic so it is unit-testable on every platform;
// the registry I/O lives in the windows-tagged file.
func migrateImagePathValue(imagePath string) (string, bool) {
	trimmed := strings.TrimSpace(imagePath)
	if trimmed == "" {
		return imagePath, false
	}

	var exe, rest string
	if strings.HasPrefix(trimmed, `"`) {
		end := strings.Index(trimmed[1:], `"`)
		if end < 0 {
			return imagePath, false // malformed; do not touch
		}
		exe = trimmed[:end+2]
		rest = strings.TrimSpace(trimmed[end+2:])
	} else {
		// Unquoted: the executable token ends at the first space after
		// the .exe suffix (paths with spaces are always quoted by the
		// installer; an unquoted spaced path is unparseable anyway).
		idx := strings.Index(strings.ToLower(trimmed), ".exe")
		if idx < 0 {
			return imagePath, false
		}
		exe = trimmed[:idx+4]
		rest = strings.TrimSpace(trimmed[idx+4:])
	}

	first := rest
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		first = rest[:sp]
	}
	if first == "run" || isKnownSubcommand(first) {
		return imagePath, false // already migrated / explicit subcommand
	}

	if rest == "" {
		return exe + " run", true
	}
	return exe + " run " + rest, true
}

// isKnownSubcommand reports whether the token is one of the CLI's
// top-level subcommands — an ImagePath already carrying one must not
// be rewritten.
func isKnownSubcommand(token string) bool {
	switch token {
	case "run", "install", "uninstall", "start", "stop", "restart",
		"status", "version", "update", "config", "license",
		"db-monitoring", "debug-modules-list":
		return true
	}
	return false
}

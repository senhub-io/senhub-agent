//go:build darwin

package cliArgs

// canonicalConfigPathForOS returns the macOS default location for
// the agent configuration file. We use /usr/local/etc/senhub-agent/
// rather than /Library/Application Support/SenHub/ because the agent
// is a CLI/daemon, not an app bundle — Homebrew-style packagers
// expect /usr/local/etc/, which is also writable by admin without
// touching the system library directory.
func canonicalConfigPathForOS() string {
	return "/usr/local/etc/senhub-agent/agent.yaml"
}

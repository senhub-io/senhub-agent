//go:build linux

package cliArgs

// canonicalConfigPathForOS returns /etc/senhub-agent/agent.yaml on
// Linux — the FHS-canonical location for system-wide configuration
// of a host-installed daemon.
func canonicalConfigPathForOS() string {
	return "/etc/senhub-agent/agent.yaml"
}

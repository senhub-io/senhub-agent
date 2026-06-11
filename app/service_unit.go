// Hardened systemd unit emitted by `agent install` on Linux.
package app

import (
	_ "embed"
	"strings"
)

// The .deb/.rpm packages ship a hardened unit
// (packaging/systemd/senhub-agent.service): User=senhub, every Linux
// capability dropped, per-probe re-grants documented inline. That file
// is the canonical least-privilege posture for the daemon (#223); the
// CLI installer must emit the same posture instead of an implicit
// root unit. go:embed cannot reach outside the package directory, so
// the unit is embedded from a sibling copy and
// TestEmbeddedUnitMatchesPackagedUnit pins the two byte-for-byte.
//
//go:embed senhub-agent.service
var packagedSystemdUnit string

const (
	defaultServiceUser = "senhub"
	rootServiceUser    = "root"
)

// hardenedSystemdScript turns the packaged unit into the template
// kardianos/service executes at install time. The packaged file
// hardcodes the packaging install paths (/usr/bin binary,
// /etc/senhub-agent config); the CLI installer keeps the executable
// path, arguments and working directory it resolved, so those lines
// are re-templated while every hardening directive is reused verbatim.
func hardenedSystemdScript(serviceUser string) string {
	lines := strings.Split(packagedSystemdUnit, "\n")
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "ExecStart="):
			out = append(out,
				`ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}`,
				`{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{end}}`)
		case strings.HasPrefix(line, "User="):
			out = append(out, "User="+serviceUser)
		case strings.HasPrefix(line, "Group="):
			out = append(out, "Group="+serviceUser)
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

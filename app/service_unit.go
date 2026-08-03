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

// rootSystemdScript is the template `install --user root` hands to
// kardianos/service instead of its built-in one, which emits
// StartLimitInterval=/StartLimitBurst= inside [Service] under their
// legacy pre-v230 names — systemd only honours start-rate limiting
// declared in [Unit] (#577) — and hardcodes RestartSec=120. No User=
// line: the implicit root identity with full capabilities is the
// point of --user root, so the capability-dropping directives of the
// hardened unit are deliberately absent here.
const rootSystemdScript = `[Unit]
Description=SenHub Agent — infrastructure monitoring agent
Documentation=https://github.com/senhub-io/senhub-agent
After=network-online.target
Wants=network-online.target
# Start-rate limiting lives in [Unit]: systemd parses StartLimitIntervalSec/
# StartLimitBurst here, ignoring them in [Service] (#577). The window pairs
# with Restart=always so a crash-looping binary is throttled, not respun
# forever.
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}
{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{end}}
# always (not on-failure): a self-update exits 0 to hand off to the new
# binary, so systemd must restart on a clean exit too (#567). An explicit
# systemctl stop is still honoured (no restart).
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
`

// linuxSystemdScript selects the unit template the Linux installer
// hands to kardianos/service. Both templates keep the start-rate
// limiting in [Unit] (#577); falling back to kardianos's built-in
// script is never correct for this agent.
func linuxSystemdScript(serviceUser string) string {
	if serviceUser == rootServiceUser {
		return rootSystemdScript
	}
	return hardenedSystemdScript(serviceUser)
}

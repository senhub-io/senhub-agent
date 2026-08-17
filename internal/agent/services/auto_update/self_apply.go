package auto_update

import "runtime"

// selfApplyRefusal returns the reason this updater must not replace the agent
// binary, or "" when it may.
func (a *autoUpdate) selfApplyRefusal() string {
	return selfApplyRefusalFor(runtime.GOOS, a.operatorDriven)
}

// selfApplyRefusalFor is the platform decision, with the OS injected so both
// branches are testable from any development machine and on every CI runner —
// a security rule that can only be exercised on the platform it protects is a
// rule nobody checks.
//
// The rule: on Linux, only the root-run `senhub-agent update` CLI installs. The
// daemon never does, and that is a security property rather than a limitation
// (#794).
//
// The daemon is the process exposed to untrusted input — OTLP arriving off the
// network, SNMP traps, syslog, tailed log files, responses from every probed
// target — so it is the component most likely to be compromised. A daemon that
// can rewrite its own executable hands whoever compromises it persistence
// across restarts, which on an agent that runs everywhere and restarts itself
// is the outcome worth attacking for. The signature check does not save it: the
// check runs inside that same process, so an attacker who owns the daemon owns
// the code doing the checking. Verification by the party that may be
// compromised is not verification.
//
// Installing is therefore left to something the daemon cannot influence — root,
// at the operator's request today, and apt/dnf/zypper once the packages exist,
// which verify against the system keyring and record what they installed.
//
// Windows is not affected and deliberately keeps in-process apply:
//
//   - an MSI install stages a signed MSI and hands off to msiexec, a privileged
//     installer outside the agent — the same shape this leaves Linux with,
//     reached by a different road;
//   - a ZIP install runs as LocalSystem, where a process rewriting its own
//     binary escalates nothing because it already holds the highest privilege
//     on the machine. There is no unprivileged service account to protect.
func selfApplyRefusalFor(goos string, operatorDriven bool) string {
	if operatorDriven || goos != "linux" {
		return ""
	}
	return "A newer version is available. The agent does not install it itself on Linux: " +
		"the binary is root-owned so the service account cannot rewrite it. " +
		"Apply it with 'sudo senhub-agent update', or through the package manager once packages are available."
}

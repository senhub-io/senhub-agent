// Reconciliation between the two agent binaries a hardened Linux install
// carries: the CLI copy an operator invokes from PATH, and the copy the
// systemd unit execs (managedBinaryDir, service-user-owned).
//
// The split is required by the non-root design (#223): the daemon runs as
// the unprivileged service user, so it can replace its OWN copy during
// auto-update but can never write the root-owned CLI copy — and must not
// be able to, or the service account could plant a binary root later runs.
// The consequence is that the two copies drift silently (#723): the CLI's
// `update` replaced only itself, and `--version` then answered for a
// binary that is not the one monitoring the host.
//
// The rule this file implements: `update`, which runs as root, is the one
// path that can write both, so it reconciles the service copy; `--version`
// reports the skew instead of hiding it. Version readout is done by
// PARSING the other binary, never by executing it — running a
// service-user-owned binary as root would reintroduce exactly the
// escalation the non-root unit exists to prevent.
package app

import (
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// versionLdflag is the tail of the -X linker flag the Makefile uses to
// stamp the version (`-X '<pkg>/cliArgs.Version=X.Y.Z'`). Matching on the
// suffix keeps the lookup working if the module path ever changes.
const versionLdflag = "cliArgs.Version="

// serviceBinaryFromUnit returns the binary path a systemd unit execs, or
// "" when the unit carries no usable ExecStart.
func serviceBinaryFromUnit(unit string) string {
	execLine, _ := installedExecStart(unit)
	if execLine == "" {
		return ""
	}
	binPath, _ := splitExecStartLine(execLine)
	if binPath == "" {
		return ""
	}
	return unescapeSystemdPath(binPath)
}

// serviceBinaryTarget returns the service binary that must be kept in sync
// with selfPath, and the user that has to own it. Both are "" when there
// is nothing to reconcile:
//
//   - the unit has no ExecStart, or none is installed;
//   - the unit execs this very binary (single-copy / legacy root install),
//     detected with os.SameFile so a symlinked PATH entry counts as one;
//   - the ExecStart target does not exist. A unit pointing at a vanished
//     binary is a 203/EXEC repair for `refresh-unit`, and creating the
//     file here would resurrect a path the operator moved away from.
//
// stat is injected so the decision is testable without a systemd install.
func serviceBinaryTarget(unit, selfPath string, stat func(string) (os.FileInfo, error)) (target, owner string) {
	bin := serviceBinaryFromUnit(unit)
	if bin == "" || selfPath == "" {
		return "", ""
	}
	selfInfo, err := stat(selfPath)
	if err != nil {
		return "", ""
	}
	binInfo, err := stat(bin)
	if err != nil {
		return "", ""
	}
	if os.SameFile(selfInfo, binInfo) {
		return "", ""
	}
	return bin, installedServiceUser(unit)
}

// binaryVersion reports the agent version stamped into the Go binary at
// path, or "" when it cannot be read.
//
// It reads the build metadata section (debug/buildinfo), which survives
// the release build's -s -w stripping and records the -ldflags the binary
// was linked with. Executing the binary with --version would be the
// obvious alternative and is deliberately NOT used: the service copy is
// owned by the unprivileged service user, and the callers here run as
// root.
func binaryVersion(path string) string {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "-ldflags" {
			return versionFromLdflags(setting.Value)
		}
	}
	return ""
}

// versionFromLdflags extracts the stamped version out of a recorded
// -ldflags string. The value is quoted by the Makefile
// (`-X 'pkg.Version=0.5.3'`) but an unquoted build is accepted too, so the
// terminator is "quote or whitespace or end".
func versionFromLdflags(ldflags string) string {
	idx := strings.Index(ldflags, versionLdflag)
	if idx < 0 {
		return ""
	}
	value := ldflags[idx+len(versionLdflag):]
	if cut := strings.IndexAny(value, "'\" \t"); cut >= 0 {
		value = value[:cut]
	}
	return value
}

// binaryFS is the set of filesystem operations reconcileServiceBinary
// needs, injected so the reconciliation is testable without a systemd
// install and without running as root.
type binaryFS struct {
	stat  func(string) (os.FileInfo, error)
	copy  func(src, dst string) error
	chown func(path, owner string) error
}

// reconcileServiceBinary replaces the binary the unit execs with the
// freshly installed release at newBinary, and returns the path it wrote
// ("" when there was nothing to reconcile). Skips are explained on out.
//
// newBinary must be the path `update` resolved BEFORE applying the
// release: the updater renames the running file out of the way and writes
// the new one at the original path, so afterwards os.Executable() resolves
// to the old — usually already deleted — inode. A source that is not on
// disk is therefore an error, not a skip: it means the caller passed a
// post-update path, and failing loudly beats silently leaving the service
// on the old binary.
func reconcileServiceBinary(unit, newBinary string, fs binaryFS, out io.Writer) (string, error) {
	if _, err := fs.stat(newBinary); err != nil {
		return "", fmt.Errorf("the updated binary is not readable at %s: %w", newBinary, err)
	}

	target, owner := serviceBinaryTarget(unit, newBinary, fs.stat)
	if target == "" {
		return "", nil
	}
	if ok, why := serviceBinaryNeedsSync(binaryVersion(newBinary), binaryVersion(target)); !ok {
		if why != "" {
			fmt.Fprintf(out, "Service binary: %s\n", why)
		}
		return "", nil
	}
	if err := fs.copy(newBinary, target); err != nil {
		return "", fmt.Errorf("staging the updated binary to %s: %w", target, err)
	}
	if err := fs.chown(target, owner); err != nil {
		return "", err
	}
	return target, nil
}

// serviceBinaryNeedsSync reports whether the service copy must be
// replaced by the CLI copy, and why not when it must not. The reason is
// operator-facing; it is empty when the skip needs no explanation.
//
// Two cases are skipped. A service copy already on the same version has
// nothing to gain from the copy, and reporting a refresh that changed
// nothing would be a lie. A service copy strictly NEWER must be refused:
// the daemon self-updates ahead of the CLI (its copy is the only one it
// can write), so copying an older CLI binary over it would downgrade the
// running agent behind the operator's back — something auto-update itself
// never does (shouldUpdateTo is strict-greater-than).
//
// An unreadable version on either side is not a reason to skip: the
// operator asked for a specific release and this copy is what delivers it.
func serviceBinaryNeedsSync(cliVersion, serviceVersion string) (bool, string) {
	if cliVersion != "" && cliVersion == serviceVersion {
		return false, ""
	}
	if cliVersion == "" || serviceVersion == "" {
		return true, ""
	}
	cli, err := goversion.NewVersion(cliVersion)
	if err != nil {
		return true, ""
	}
	service, err := goversion.NewVersion(serviceVersion)
	if err != nil {
		return true, ""
	}
	if service.GreaterThan(cli) {
		return false, fmt.Sprintf(
			"the systemd service already runs %s, which is newer than %s — left untouched",
			serviceVersion, cliVersion)
	}
	return true, ""
}

// serviceBinarySkewNote renders the warning `--version` appends when the
// systemd service runs a different build than the CLI binary, or "" when
// there is nothing to report (no service copy, unreadable version, or
// both copies on the same version).
func serviceBinarySkewNote(cliVersion, serviceVersion, path string) string {
	if serviceVersion == "" || path == "" || serviceVersion == cliVersion {
		return ""
	}
	return fmt.Sprintf(
		"Service binary: %s (%s)\n"+
			"Note: the systemd service runs a different build than this CLI binary.\n"+
			"      'sudo senhub-agent update <version>' updates both copies.\n",
		serviceVersion, path)
}

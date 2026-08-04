package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The -ldflags string as the release build records it, straight out of a
// 0.5.3 binary's build metadata (trimmed to the flags that matter here).
const releaseLdflags = `-s -w -X 'senhub-agent.go/internal/agent/cliArgs.Version=0.5.3' ` +
	`-X 'senhub-agent.go/internal/agent/cliArgs.CommitHash=0.5.3-12-gabc1234' ` +
	`-X 'senhub-agent.go/internal/agent/services/auto_update.signingPublicKey=RWRlfky'`

func TestVersionFromLdflags(t *testing.T) {
	cases := []struct {
		name    string
		ldflags string
		want    string
	}{
		{"release build", releaseLdflags, "0.5.3"},
		{"beta version", `-X 'senhub-agent.go/internal/agent/cliArgs.Version=0.5.4-beta'`, "0.5.4-beta"},
		{"unquoted flag", `-X senhub-agent.go/internal/agent/cliArgs.Version=0.5.3 -X other=1`, "0.5.3"},
		{"last flag of the string", `-s -w -X 'pkg/cliArgs.Version=1.2.3'`, "1.2.3"},
		{"no version stamped", `-s -w -X 'pkg/cliArgs.CommitHash=abc'`, ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionFromLdflags(tc.ldflags); got != tc.want {
				t.Fatalf("versionFromLdflags(%q) = %q, want %q", tc.ldflags, got, tc.want)
			}
		})
	}
}

// TestBinaryVersion_ReadsStampedVersionWithoutExecuting pins the assumption
// the whole skew report rests on: a stripped release build (-s -w) still
// carries its -X stamped version in the Go build metadata, so the version
// of the OTHER binary can be read without running it.
func TestBinaryVersion_ReadsStampedVersionWithoutExecuting(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not in PATH; cannot build the fixture binary")
	}

	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatalf("creating fixture dir: %v", mkErr)
		}
		if wErr := os.WriteFile(path, []byte(content), 0o644); wErr != nil {
			t.Fatalf("writing %s: %v", name, wErr)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.21\n")
	write("cliArgs/vars.go", "package cliArgs\n\nvar Version = \"dev\"\n")
	write("main.go", "package main\n\nimport \"fixture/cliArgs\"\n\nfunc main() { println(cliArgs.Version) }\n")

	out := filepath.Join(dir, "fixture-agent")
	build := exec.Command(goTool, "build", "-o", out,
		"-ldflags", "-s -w -X 'fixture/cliArgs.Version=9.9.9-fixture'", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=")
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Skipf("cannot build the fixture binary in this environment: %v (%s)", buildErr, output)
	}

	if got := binaryVersion(out); got != "9.9.9-fixture" {
		t.Fatalf("binaryVersion() = %q, want %q", got, "9.9.9-fixture")
	}
}

func TestBinaryVersion_UnreadableFileReportsNothing(t *testing.T) {
	notABinary := filepath.Join(t.TempDir(), "senhub-agent")
	if err := os.WriteFile(notABinary, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if got := binaryVersion(notABinary); got != "" {
		t.Fatalf("binaryVersion(script) = %q, want \"\"", got)
	}
	if got := binaryVersion(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Fatalf("binaryVersion(missing) = %q, want \"\"", got)
	}
}

const hardenedUnit = `[Unit]
Description=SenHub Agent

[Service]
User=senhub
Group=senhub
ExecStart=/var/lib/senhub-agent/bin/senhub-agent run --config-path /etc/senhub-agent/agent.yaml
`

func TestServiceBinaryFromUnit(t *testing.T) {
	cases := []struct {
		name string
		unit string
		want string
	}{
		{"hardened unit", hardenedUnit, "/var/lib/senhub-agent/bin/senhub-agent"},
		{"no arguments", "[Service]\nExecStart=/usr/local/bin/senhub-agent\n", "/usr/local/bin/senhub-agent"},
		{"escaped space in path", "[Service]\nExecStart=/opt/sen\\x20hub/senhub-agent run\n", "/opt/sen hub/senhub-agent"},
		{"no ExecStart", "[Unit]\nDescription=x\n", ""},
		{"empty unit", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serviceBinaryFromUnit(tc.unit); got != tc.want {
				t.Fatalf("serviceBinaryFromUnit() = %q, want %q", got, tc.want)
			}
		})
	}
}

// touch creates an empty file and returns its path.
func touch(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestServiceBinaryTarget_DistinctFileIsReconciled(t *testing.T) {
	dir := t.TempDir()
	self := touch(t, dir, "cli-copy")
	service := touch(t, dir, "service-copy")

	unit := "[Service]\nUser=senhub\nExecStart=" + service + " run\n"
	target, owner := serviceBinaryTarget(unit, self, os.Stat)

	if target != service {
		t.Fatalf("target = %q, want %q", target, service)
	}
	if owner != "senhub" {
		t.Fatalf("owner = %q, want %q", owner, "senhub")
	}
}

// A legacy root install execs the very binary the operator invokes. There
// is no second copy, so `update` must not copy the file over itself.
func TestServiceBinaryTarget_SameFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	self := touch(t, dir, "senhub-agent")

	unit := "[Service]\nExecStart=" + self + " run\n"
	if target, _ := serviceBinaryTarget(unit, self, os.Stat); target != "" {
		t.Fatalf("target = %q, want \"\" for a unit execing the running binary", target)
	}
}

// A PATH entry symlinked to the managed copy is the same file too — string
// comparison would miss it, os.SameFile does not.
func TestServiceBinaryTarget_SymlinkToSameFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	service := touch(t, dir, "managed")
	link := filepath.Join(dir, "senhub-agent")
	if err := os.Symlink(service, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	unit := "[Service]\nExecStart=" + service + " run\n"
	if target, _ := serviceBinaryTarget(unit, link, os.Stat); target != "" {
		t.Fatalf("target = %q, want \"\" when the PATH entry links to the service copy", target)
	}
}

// A unit pointing at a binary that no longer exists is a 203/EXEC repair
// for refresh-unit; update must not resurrect the path.
func TestServiceBinaryTarget_MissingTargetIsSkipped(t *testing.T) {
	dir := t.TempDir()
	self := touch(t, dir, "cli-copy")

	unit := "[Service]\nExecStart=" + filepath.Join(dir, "gone") + " run\n"
	if target, _ := serviceBinaryTarget(unit, self, os.Stat); target != "" {
		t.Fatalf("target = %q, want \"\" when the ExecStart binary is missing", target)
	}
}

func TestServiceBinaryTarget_NoUnitIsSkipped(t *testing.T) {
	self := touch(t, t.TempDir(), "cli-copy")
	if target, _ := serviceBinaryTarget("", self, os.Stat); target != "" {
		t.Fatalf("target = %q, want \"\" without a unit", target)
	}
}

func TestServiceBinaryNeedsSync(t *testing.T) {
	cases := []struct {
		name       string
		cli        string
		service    string
		wantSync   bool
		wantReason string
	}{
		{name: "service older is refreshed", cli: "0.5.4", service: "0.5.2", wantSync: true},
		{name: "same version has nothing to refresh", cli: "0.5.4", service: "0.5.4", wantSync: false},
		{name: "service newer is refused", cli: "0.5.2", service: "0.5.4", wantSync: false, wantReason: "0.5.4"},
		{name: "service on a newer beta is refused", cli: "0.5.3", service: "0.5.4-beta", wantSync: false, wantReason: "0.5.4-beta"},
		{name: "beta older than stable is refreshed", cli: "0.5.4", service: "0.5.4-beta", wantSync: true},
		{name: "unreadable service version proceeds", cli: "0.5.4", service: "", wantSync: true},
		{name: "unparseable version proceeds", cli: "0.5.4", service: "not-a-version", wantSync: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := serviceBinaryNeedsSync(tc.cli, tc.service)
			if got != tc.wantSync {
				t.Fatalf("serviceBinaryNeedsSync(%q, %q) = %v, want %v", tc.cli, tc.service, got, tc.wantSync)
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to mention %q", reason, tc.wantReason)
			}
			if tc.wantReason == "" && reason != "" {
				t.Fatalf("reason = %q, want none — only a refused copy is explained", reason)
			}
		})
	}
}

// recordingFS is a binaryFS that performs no I/O beyond stat and records
// what the reconciliation asked for.
type recordingFS struct {
	copiedFrom  string
	copiedTo    string
	chownedPath string
	chownedTo   string
	copyErr     error
}

func (r *recordingFS) ops() binaryFS {
	return binaryFS{
		stat: os.Stat,
		copy: func(src, dst string) error {
			r.copiedFrom, r.copiedTo = src, dst
			return r.copyErr
		},
		chown: func(path, owner string) error {
			r.chownedPath, r.chownedTo = path, owner
			return nil
		},
	}
}

func TestReconcileServiceBinary_CopiesAndRestoresOwnership(t *testing.T) {
	dir := t.TempDir()
	newBinary := touch(t, dir, "new-release")
	service := touch(t, dir, "service-copy")
	unit := "[Service]\nUser=senhub\nExecStart=" + service + " run\n"

	fs := &recordingFS{}
	written, err := reconcileServiceBinary(unit, newBinary, fs.ops(), io.Discard)
	if err != nil {
		t.Fatalf("reconcileServiceBinary() error = %v", err)
	}

	if written != service {
		t.Fatalf("written = %q, want %q", written, service)
	}
	if fs.copiedFrom != newBinary || fs.copiedTo != service {
		t.Fatalf("copied %q -> %q, want %q -> %q", fs.copiedFrom, fs.copiedTo, newBinary, service)
	}
	// Without this the root-owned copy locks the unprivileged daemon out of
	// its own binary and every later auto-update fails (#377/#571).
	if fs.chownedPath != service || fs.chownedTo != "senhub" {
		t.Fatalf("chowned %q to %q, want %q to %q", fs.chownedPath, fs.chownedTo, service, "senhub")
	}
}

// Regression: the reconciliation must be handed the path resolved BEFORE
// the update. selfupdate renames the running binary out of the way and
// deletes it, so a caller passing os.Executable() afterwards hands over a
// vanished path — which silently reconciled nothing until this guard.
func TestReconcileServiceBinary_VanishedSourceIsAnError(t *testing.T) {
	dir := t.TempDir()
	service := touch(t, dir, "service-copy")
	unit := "[Service]\nUser=senhub\nExecStart=" + service + " run\n"

	fs := &recordingFS{}
	written, err := reconcileServiceBinary(unit, filepath.Join(dir, ".new-release.old"), fs.ops(), io.Discard)

	if err == nil {
		t.Fatal("reconcileServiceBinary() error = nil, want an error for a source that is not on disk")
	}
	if written != "" {
		t.Fatalf("written = %q, want \"\"", written)
	}
	if fs.copiedTo != "" {
		t.Fatalf("copied to %q, want no copy attempted", fs.copiedTo)
	}
}

func TestReconcileServiceBinary_NoUnitTargetIsANoOp(t *testing.T) {
	dir := t.TempDir()
	newBinary := touch(t, dir, "new-release")

	fs := &recordingFS{}
	written, err := reconcileServiceBinary("[Unit]\nDescription=x\n", newBinary, fs.ops(), io.Discard)
	if err != nil {
		t.Fatalf("reconcileServiceBinary() error = %v", err)
	}
	if written != "" || fs.copiedTo != "" {
		t.Fatalf("written = %q, copied to %q; want a no-op without an ExecStart", written, fs.copiedTo)
	}
}

func TestReconcileServiceBinary_CopyFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	newBinary := touch(t, dir, "new-release")
	service := touch(t, dir, "service-copy")
	unit := "[Service]\nUser=senhub\nExecStart=" + service + " run\n"

	fs := &recordingFS{copyErr: os.ErrPermission}
	if _, err := reconcileServiceBinary(unit, newBinary, fs.ops(), io.Discard); err == nil {
		t.Fatal("reconcileServiceBinary() error = nil, want the copy failure surfaced")
	}
	if fs.chownedPath != "" {
		t.Fatalf("chowned %q after a failed copy, want none", fs.chownedPath)
	}
}

func TestServiceBinarySkewNote(t *testing.T) {
	const path = "/var/lib/senhub-agent/bin/senhub-agent"

	note := serviceBinarySkewNote("0.4.1", "0.5.3", path)
	if !strings.Contains(note, "0.5.3") || !strings.Contains(note, path) {
		t.Fatalf("note = %q, want it to name the service version and path", note)
	}

	if got := serviceBinarySkewNote("0.5.3", "0.5.3", path); got != "" {
		t.Fatalf("note = %q, want none when both copies match", got)
	}
	if got := serviceBinarySkewNote("0.5.3", "", ""); got != "" {
		t.Fatalf("note = %q, want none when there is no service copy", got)
	}
}

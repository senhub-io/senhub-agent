//go:build boot_smoke

// Package main — boot smoke tests.
//
// These tests build the agent binary once, then exec it against the
// example configurations shipped in examples/. They guard the most
// common bring-up regressions:
//
//   - the binary links and runs (no missing C dep, no panic on init)
//   - `agent version` exits cleanly
//   - `agent config check <example>` parses every shipped example
//     without raising [ERROR] lines (warnings are acceptable)
//
// Gated behind the `boot_smoke` build tag so the regular `make test`
// (and `make test-race`) stays fast and avoids a self-inflicted race
// where the inner `go build` invoked here would contend with the
// outer `go test ./...` for the build cache under -race. Run from CI
// via a dedicated `go test -tags=boot_smoke ./cmd/agent/...` step, or
// locally via `make boot-smoke`.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildAgent builds the senhub-agent binary once per test process and
// returns its absolute path. Failing here is a hard stop — every other
// boot smoke test depends on having a runnable binary, so we make the
// error visible at the top of the report instead of letting each
// subtest fail with a confusing "binary not found" line.
func buildAgent(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "senhub-agent-smoke")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("building senhub-agent for smoke test: %v", err)
	}
	return bin
}

// repoRoot returns the repository root so the test can reach the
// `examples/` directory regardless of where `go test` is invoked.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	// cmd/agent → repo root is two levels up.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// execAgent invokes the binary with the given args and returns combined
// stdout + stderr plus the exit error.
func execAgent(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func TestBootSmoke_Version(t *testing.T) {
	bin := buildAgent(t)
	out, err := execAgent(t, bin, "version")
	if err != nil {
		t.Fatalf("`senhub-agent version` returned error: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Version") {
		t.Errorf("`senhub-agent version` output missing the word 'Version'.\noutput:\n%s", out)
	}
}

// TestBootSmoke_VersionJSON pins the machine-readable contract on the
// real binary: one JSON object on stdout, with the schema id.
func TestBootSmoke_VersionJSON(t *testing.T) {
	bin := buildAgent(t)
	out, err := execAgent(t, bin, "version", "--json")
	if err != nil {
		t.Fatalf("`senhub-agent version --json` returned error: %v\noutput:\n%s", err, out)
	}
	var doc struct {
		Schema string `json:"schema"`
		OK     bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("`senhub-agent version --json` is not one JSON object: %v\noutput:\n%s", err, out)
	}
	if doc.Schema != "senhub.cli.version/v1" || !doc.OK {
		t.Errorf("unexpected document: %+v", doc)
	}
}

// TestBootSmoke_VersionFlag pins the post-#134 contract:
// `senhub-agent --version` prints the version and exits 0 instead of
// spawning a full agent. Pre-fix it fell through to `run`, racing
// the systemd-managed service for the listener port.
func TestBootSmoke_VersionFlag(t *testing.T) {
	bin := buildAgent(t)
	out, err := execAgent(t, bin, "--version")
	if err != nil {
		t.Fatalf("`senhub-agent --version` returned error: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "Version") {
		t.Errorf("`senhub-agent --version` output missing 'Version':\n%s", out)
	}
	if strings.Contains(out, "Starting") || strings.Contains(out, "Initializing agent") {
		t.Errorf("`senhub-agent --version` should NOT start the agent:\n%s", out)
	}
}

// TestBootSmoke_UnknownArgRejected pins the closed-set rejection of
// unknown top-level args. Pre-fix the parser silently routed
// anything unknown to `run`, allowing a typo to spawn an agent.
func TestBootSmoke_UnknownArgRejected(t *testing.T) {
	bin := buildAgent(t)
	out, err := execAgent(t, bin, "--definitely-not-a-flag")
	if err == nil {
		t.Fatalf("`senhub-agent --definitely-not-a-flag` should exit non-zero — got success.\noutput:\n%s", out)
	}
	if !strings.Contains(out, "unknown command or flag") {
		t.Errorf("error output should mention 'unknown command or flag':\n%s", out)
	}
	if strings.Contains(out, "Starting") || strings.Contains(out, "Initializing agent") {
		t.Errorf("unknown flag should NOT start the agent:\n%s", out)
	}
}

// TestBootSmoke_ConfigCheckFreeTier exercises `agent config check` on
// the free-tier example. Free-tier is the only example we can assert
// is fully error-free out of the box: the Pro / Enterprise / grace
// examples ship with placeholder JWT tokens whose signatures don't
// validate, so they intentionally report `[ERROR] agent.license:
// invalid` when fed to `config check`. Validating those would require
// minting a real test JWT, which is out of scope for a bring-up smoke.
func TestBootSmoke_ConfigCheckFreeTier(t *testing.T) {
	bin := buildAgent(t)
	cfg := filepath.Join(repoRoot(t), "examples", "example-config-free-tier.yaml")

	out, err := execAgent(t, bin, "config", "check", cfg)
	if err != nil {
		t.Fatalf("`senhub-agent config check %s` failed: %v\noutput:\n%s",
			cfg, err, out)
	}
	if strings.Contains(out, "[ERROR]") {
		t.Errorf("`senhub-agent config check %s` reported [ERROR] lines:\n%s", cfg, out)
	}
	if !strings.Contains(out, "Configuration is valid") {
		t.Errorf("`senhub-agent config check %s` did not report a valid configuration:\n%s",
			cfg, out)
	}
}

// TestBootSmoke_DoctorJSON runs the diagnosis on the free-tier example
// with no service installed. The exit code depends on the host (a
// warning for the missing service, a failure if port 8080 is taken), so
// the contract pinned here is the document and the absence of a crash.
func TestBootSmoke_DoctorJSON(t *testing.T) {
	bin := buildAgent(t)
	example, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "example-config-free-tier.yaml"))
	if err != nil {
		t.Fatalf("reading the free-tier example: %v", err)
	}
	cfg := filepath.Join(t.TempDir(), "agent-config.yaml")
	if err := os.WriteFile(cfg, example, 0o600); err != nil {
		t.Fatalf("writing the config copy: %v", err)
	}

	cmd := exec.Command(bin, "doctor", "--json", "--config-path", cfg)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("`senhub-agent doctor --json` did not run: %v", runErr)
		}
		code = exitErr.ExitCode()
	}

	var doc struct {
		Schema   string `json:"schema"`
		ExitCode int    `json:"exit_code"`
		Checks   []struct {
			Section string `json:"section"`
			ID      string `json:"id"`
			Level   string `json:"level"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("`senhub-agent doctor --json` is not one JSON object: %v\noutput:\n%s", err, stdout.String())
	}
	if doc.Schema != "senhub.cli.doctor/v1" {
		t.Errorf("schema %q", doc.Schema)
	}
	if code > 2 || doc.ExitCode != code {
		t.Errorf("process exit code %d, document says %d", code, doc.ExitCode)
	}
	if len(doc.Checks) == 0 {
		t.Error("the document carries no check")
	}
	for _, c := range doc.Checks {
		if c.ID == "config.check" && c.Level != "ok" {
			t.Errorf("the free-tier example is not reported valid: %+v", c)
		}
	}
}

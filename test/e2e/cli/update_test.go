//go:build e2e

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestUpdateRejectsAnUnreadableVersion(t *testing.T) {
	t.Parallel()
	// ent#921, 0.7.0-rc.5: malformed versions are rejected before any registry lookup.
	for _, v := range []string{"pas-une-version", "0.7.0.rc.4", "9.x.zz"} {
		copy := copyBin(t, bin)
		stdout, stderr, code := run(t, copy, "update", v)
		out := stdout + stderr
		if code == 0 {
			t.Fatalf("update %s: expected non-zero exit, got 0", v)
		}
		if !strings.Contains(out, "not a valid version: "+v) {
			t.Fatalf("update %s: output missing invalid-version message:\n%s", v, out)
		}
		if strings.Contains(out, "Already up to date") {
			t.Fatalf("update %s: output must not claim up to date:\n%s", v, out)
		}
	}
}

func TestUpdateReportsAVersionTheRegistryDoesNotHave(t *testing.T) {
	t.Parallel()
	// ent#921, 0.7.0-rc.5: unknown versions are reported as registry misses.
	for _, v := range []string{"9.9.9", "0.0.1-nexiste-pas"} {
		stdout, stderr, code := run(t, bin, "update", v)
		out := stdout + stderr
		if code == 0 {
			t.Fatalf("update %s: expected non-zero exit, got 0", v)
		}
		if !strings.Contains(out, "not found in the update registry") {
			t.Fatalf("update %s: output missing registry miss:\n%s", v, out)
		}
	}
	stdout, stderr, code := run(t, bin, "update", "0.7.0-beta.99")
	out := stdout + stderr
	if code == 0 {
		t.Fatal("update 0.7.0-beta.99: expected non-zero exit, got 0")
	}
	if !strings.Contains(out, "cannot fetch version 0.7.0-beta.99") {
		t.Fatalf("update 0.7.0-beta.99: output missing fetch error:\n%s", out)
	}
}

func TestUpdateReportsAnUnreachableRegistry(t *testing.T) {
	t.Parallel()
	// ent#921, 0.7.0-rc.5: registry network failures surface as errors, not as no-op success.
	stdout, stderr, code := run(t, bin, "update", "0.6.5", "--registry-url", "https://127.0.0.1:1")
	out := stdout + stderr
	if code == 0 {
		t.Fatal("expected non-zero exit for unreachable registry")
	}
	if !strings.Contains(out, "cannot") {
		t.Fatalf("output missing cannot error:\n%s", out)
	}
	if strings.Contains(out, "Already up to date") {
		t.Fatalf("output must not claim up to date:\n%s", out)
	}
}

func TestUpdateToTheRunningVersion(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := run(t, bin, "update", "0.7.0-rc.5")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Already up to date.") {
		t.Fatalf("output missing up-to-date message:\n%s", out)
	}
}

func TestUpdateDryRunChangesNothing(t *testing.T) {
	t.Parallel()
	// ent#924, 0.7.0-rc.5: --dry-run must not replace the binary or print install/restart actions.
	copy := copyBin(t, bin)
	before, err := os.ReadFile(copy)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, copy, "update", "0.6.5", "--dry-run")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Dry run: would install 0.6.5 over 0.7.0-rc.5; no change made.") {
		t.Fatalf("output missing dry-run message:\n%s", out)
	}
	if strings.Contains(out, "Update installed") || strings.Contains(out, "Restart") {
		t.Fatalf("dry run must not report install/restart:\n%s", out)
	}
	after, err := os.ReadFile(copy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry run changed the binary")
	}
}

func TestUpdateLatestFollowsTheChannel(t *testing.T) {
	// ent#925, 0.7.0-rc.5: latest follows stable/beta channel from include_beta.
	withIncludeBeta(t, false)
	stdout, stderr, code := run(t, bin, "update", "latest", "--dry-run")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("stable latest: expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "nothing to install over 0.7.0-rc.5") {
		t.Fatalf("stable latest: output missing no-op message:\n%s", out)
	}
	t.Run("beta", func(t *testing.T) {
		withIncludeBeta(t, true)
		stdout, stderr, code := run(t, bin, "update", "latest", "--dry-run")
		out := stdout + stderr
		if code != 0 {
			t.Fatalf("beta latest: expected zero exit, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "would install 0.7.0-rc.6 over 0.7.0-rc.5") {
			t.Fatalf("beta latest: output missing beta install message:\n%s", out)
		}
	})
}

func TestUpdateConstraintNeverGoesBackwards(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := run(t, bin, "update", ">=0.6", "--dry-run")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "nothing to install over 0.7.0-rc.5") {
		t.Fatalf("output missing no-op message:\n%s", out)
	}
}

func TestUpdateListShowsEachReleaseOnce(t *testing.T) {
	// ent#933, 0.7.0-rc.5: --list deduplicates releases and hides beta section when disabled.
	withIncludeBeta(t, true)
	stdout, stderr, code := run(t, bin, "update", "--list")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("beta list: expected zero exit, got %d:\n%s", code, out)
	}
	if strings.Count(out, "0.7.0-rc.6") != 1 {
		t.Fatalf("beta list: 0.7.0-rc.6 must appear once:\n%s", out)
	}
	if !strings.Contains(out, "* 0.7.0-rc.5") {
		t.Fatalf("beta list: missing current marker line:\n%s", out)
	}
	if strings.Count(out, "0.6.5") != 1 {
		t.Fatalf("beta list: 0.6.5 must appear once:\n%s", out)
	}
	if strings.Contains(out, "latest") {
		t.Fatalf("beta list: must not contain latest alias:\n%s", out)
	}
	withIncludeBeta(t, false)
	stdout, stderr, code = run(t, bin, "update", "--list")
	out = stdout + stderr
	if code != 0 {
		t.Fatalf("stable list: expected zero exit, got %d:\n%s", code, out)
	}
	if strings.Contains(out, "Beta releases") {
		t.Fatalf("stable list: must not contain Beta releases:\n%s", out)
	}
}

func TestUpdateInstallsAnOlderVersion(t *testing.T) {
	t.Parallel()
	// ent#921, 0.7.0-rc.5: explicit downgrade installs and the new binary reports its version.
	copy := copyBin(t, bin)
	stdout, stderr, code := run(t, copy, "update", "0.6.5", "--no-restart", "--no-refresh-unit")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Update installed") {
		t.Fatalf("output missing install confirmation:\n%s", out)
	}
	stdout, stderr, code = run(t, copy, "--version")
	out = stdout + stderr
	if code != 0 {
		t.Fatalf("version after update: expected zero exit, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "0.6.5") {
		t.Fatalf("version after update must report 0.6.5:\n%s", out)
	}
}

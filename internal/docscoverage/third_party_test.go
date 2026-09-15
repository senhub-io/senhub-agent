package docscoverage

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The third-party list is an annexe to the licence agreement, which promises
// the customer the components of the version they run. A list that drifts
// from the binary makes that promise false, and the drift is invisible: a
// dependency is added in a pull request nobody reads for its go.sum.
//
// The guard regenerates into a temporary copy and compares. It skips when the
// module cache is unreachable, since that is an environment failure and not a
// drift.
func TestThirdPartyNoticesMatchTheBuild(t *testing.T) {
	root := repoRoot(t)
	current, err := os.ReadFile(filepath.Join(root, "THIRD-PARTY-NOTICES.md"))
	if err != nil {
		t.Fatalf("THIRD-PARTY-NOTICES.md is missing; run `make third-party-notices`: %v", err)
	}

	cmd := exec.Command("python3", filepath.Join(root, "scripts", "third-party-notices.py"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SENHUB_NOTICES_OUT="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "cannot find module") ||
			strings.Contains(string(out), "dial tcp") {
			t.Skipf("module cache unreachable: %s", out)
		}
		t.Fatalf("regenerating the notices failed: %v\n%s", err, out)
	}

	regenerated, err := os.ReadFile(filepath.Join(root, "THIRD-PARTY-NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(regenerated) != string(current) {
		if err := os.WriteFile(filepath.Join(root, "THIRD-PARTY-NOTICES.md"), current, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatal("THIRD-PARTY-NOTICES.md no longer matches the binary's dependencies; " +
			"run `make third-party-notices` and commit the result")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

//go:build integration

package bridge

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestIntegration_Bridge_PUB400 connects to the real pub400.com using
// credentials supplied via environment variables. Run with:
//
//	PUB400_USER=... PUB400_PASSWORD=... \
//	    go test -tags=integration ./internal/probes/ibmi/bridge/
//
// The test is skipped if either credential is missing so CI without
// secrets stays green.
func TestIntegration_Bridge_PUB400(t *testing.T) {
	user := os.Getenv("PUB400_USER")
	password := os.Getenv("PUB400_PASSWORD")
	if user == "" || password == "" {
		t.Skip("PUB400_USER and PUB400_PASSWORD must be set for integration tests")
	}

	runnerDir := runnerDirForTest(t)
	ensureRunnerAssets(t, runnerDir)

	cfg := Config{
		Host:      "pub400.com",
		User:      user,
		Password:  password,
		RunnerDir: runnerDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(context.Background())

	const sql = `SELECT ELAPSED_CPU_USED, CONFIGURED_CPUS FROM QSYS2.SYSTEM_STATUS_INFO`
	res, err := br.Query(ctx, sql)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d: %v", len(res.Columns), res.Columns)
	}
	if len(res.Rows) == 0 {
		t.Fatal("expected at least one row")
	}
	t.Logf("columns=%v rows[0]=%v", res.Columns, pretty(res.Rows[0]))
}

// runnerDirForTest locates the bridge package source directory so the
// integration test can find Jt400Runner.class and jt400.jar living next
// to the .java source file. runtime.Caller gives us the test file's own
// path, and the runner assets sit in the same directory.
func runnerDirForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

// ensureRunnerAssets verifies that Jt400Runner.class and jt400.jar are
// present next to the source. This is a developer-experience guardrail:
// the first time someone runs the integration test they should see a
// clear message instead of an obscure "java.lang.ClassNotFoundException".
func ensureRunnerAssets(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"Jt400Runner.class", "jt400.jar"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing runner asset %s: %v\n"+
				"run `make bridge-assets` from the repo root to download jt400.jar "+
				"and compile Jt400Runner.java", name, err)
		}
	}
}

func pretty(row []*string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		if v == nil {
			out[i] = "<null>"
		} else {
			out[i] = *v
		}
	}
	return out
}

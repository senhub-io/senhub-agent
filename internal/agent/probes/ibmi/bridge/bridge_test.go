package bridge

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a dispatch point for the fake JT400 runner used
// by every test below. When BRIDGE_FAKE_BEHAVIOR is set in the
// environment, the test binary acts as the runner instead of executing
// the test suite. Tests spawn `os.Args[0]` via `NativeRunner`, so the
// fake works identically on Linux, macOS and Windows — no /bin/sh, no
// .exe shebang shenanigans.
//
// The bridge inherits `os.Environ()` when it spawns a subprocess
// (see bridge.go:cmd.Env). t.Setenv in each test arms a single
// behavior; subprocess inherits the var, TestMain catches it before
// m.Run() and dispatches.
func TestMain(m *testing.M) {
	if behavior := os.Getenv("BRIDGE_FAKE_BEHAVIOR"); behavior != "" {
		os.Exit(runFakeBehavior(behavior))
	}
	os.Exit(m.Run())
}

// runFakeBehavior implements the small set of canned JT400-runner
// behaviors that the bridge tests need. Each behavior maps 1:1 to one
// of the legacy shell scripts the suite used to inline.
//
// The protocol is line-oriented: bridge writes one SQL per line on the
// runner's stdin (empty line = quit signal), runner writes one JSON
// response per query on stdout. The bridge expects a single
// `{"ok":true,"event":"ready"}` line on startup before it considers
// the handshake complete.
func runFakeBehavior(name string) int {
	out := bufio.NewWriter(os.Stdout)
	in := bufio.NewReader(os.Stdin)
	emit := func(s string) {
		_, _ = out.WriteString(s)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}
	// emitReady is a no-op when the behavior is a handshake-error
	// variant; callers decide.
	emitReady := func() { emit(`{"ok":true,"event":"ready"}`) }

	// readQuery returns the next non-empty SQL line, or ("", false)
	// when the client signalled quit (empty line) or stdin EOF.
	readQuery := func() (string, bool) {
		line, err := in.ReadString('\n')
		if err != nil {
			return "", false
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}

	switch name {
	case "handshake_error":
		// One JSON error line on startup, then exit — used to test
		// the bridge's New() handshake-error path.
		emit(`{"ok":false,"error":"boom"}`)
		return 0

	case "ready_echo_ok":
		// Ready, then echo a constant OK row for every query.
		emitReady()
		for {
			if _, ok := readQuery(); !ok {
				return 0
			}
			emit(`{"ok":true,"columns":["A"],"rows":[["42"]]}`)
		}

	case "ready_echo_error":
		// Ready, then echo a runner-level error for every query.
		emitReady()
		for {
			if _, ok := readQuery(); !ok {
				return 0
			}
			emit(`{"ok":false,"error":"SQL0206 column not found"}`)
		}

	case "ready_read_one":
		// Ready, then read exactly one line and exit — used by the
		// multiline-SQL rejection test where the bridge rejects the
		// query before writing it to us.
		emitReady()
		_, _ = in.ReadString('\n')
		return 0

	case "ready_exit_on_empty":
		// Ready, then echo a constant OK row for each query, exit
		// cleanly on quit signal. Used by Close lifecycle tests.
		emitReady()
		for {
			if _, ok := readQuery(); !ok {
				return 0
			}
			emit(`{"ok":true,"columns":["A"],"rows":[["x"]]}`)
		}

	case "ready_error_then_ok":
		// First query → runner-level error (semantic, not transport);
		// subsequent queries → OK. Verifies that a semantic error
		// doesn't tear the bridge down.
		emitReady()
		first := true
		for {
			if _, ok := readQuery(); !ok {
				return 0
			}
			if first {
				emit(`{"ok":false,"error":"SQL0204 table not found"}`)
				first = false
			} else {
				emit(`{"ok":true,"columns":["A"],"rows":[["42"]]}`)
			}
		}

	case "one_query_then_exit":
		// Ready, answer one query, exit(1). The bridge must detect
		// the death on the next call and respawn (which lands back
		// in this same behavior — same canned answer).
		emitReady()
		if _, ok := readQuery(); ok {
			emit(`{"ok":true,"columns":["A"],"rows":[["first-run"]]}`)
		}
		return 1

	default:
		fmt.Fprintf(os.Stderr, "unknown BRIDGE_FAKE_BEHAVIOR: %s\n", name)
		return 2
	}
}

// newFakeRunner arms the test binary to act as the JT400 runner for
// the duration of the test (via t.Setenv) and returns a writable
// `runnerDir` that satisfies the bridge's config contract. The actual
// runner binary is `os.Args[0]` — the test binary itself, wired
// through Config.NativeRunner so the bridge skips the Java-home
// lookup.
func newFakeRunner(t *testing.T, behavior string) (runnerDir string) {
	t.Helper()
	root := t.TempDir()
	runnerDir = filepath.Join(root, "runner")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatalf("mkdir runnerDir: %v", err)
	}
	t.Setenv("BRIDGE_FAKE_BEHAVIOR", behavior)
	return runnerDir
}

func baseConfig(runnerDir string) Config {
	return Config{
		Host:           "example.test",
		User:           "USR",
		Password:       "PWD",
		NativeRunner:   os.Args[0],
		RunnerDir:      runnerDir,
		StartupTimeout: 2 * time.Second,
		QueryTimeout:   2 * time.Second,
	}
}

func TestNew_HandshakeAndQuery(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_echo_ok")
	cfg := baseConfig(runnerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(context.Background())

	res, err := br.Query(ctx, "SELECT 1 FROM SYSIBM.SYSDUMMY1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "A" {
		t.Fatalf("unexpected columns: %#v", res.Columns)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] == nil || *res.Rows[0][0] != "42" {
		t.Fatalf("unexpected rows: %#v", res.Rows)
	}
}

func TestNew_HandshakeError(t *testing.T) {
	runnerDir := newFakeRunner(t, "handshake_error")
	cfg := baseConfig(runnerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := New(ctx, cfg, nil); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error did not mention runtime error: %v", err)
	}
}

func TestQuery_RejectsEmbeddedNewlines(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_read_one")
	cfg := baseConfig(runnerDir)

	ctx := context.Background()
	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(ctx)

	_, err = br.Query(ctx, "SELECT 1\nFROM DUAL")
	if err == nil {
		t.Fatal("expected error on multi-line SQL")
	}
	if !strings.Contains(err.Error(), "single-line") {
		t.Fatalf("expected single-line error, got: %v", err)
	}
}

func TestQuery_RunnerErrorResponse(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_echo_error")
	cfg := baseConfig(runnerDir)

	ctx := context.Background()
	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(ctx)

	_, err = br.Query(ctx, "SELECT BOGUS FROM SYSIBM.SYSDUMMY1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "SQL0206") {
		t.Fatalf("expected runner error to surface, got %v", err)
	}
}

func TestConfig_Validation(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing host", Config{User: "u", Password: "p", RunnerDir: "/tmp"}},
		{"missing user", Config{Host: "h", Password: "p", RunnerDir: "/tmp"}},
		{"missing password", Config{Host: "h", User: "u", RunnerDir: "/tmp"}},
		{"missing runner dir", Config{Host: "h", User: "u", Password: "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(context.Background(), tc.cfg, nil); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestClose_Idempotent(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_exit_on_empty")
	cfg := baseConfig(runnerDir)

	br, err := New(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := br.Close(context.Background()); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := br.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestQuery_RunnerErrorKeepsBridgeAlive asserts that a semantic runner
// error ({"ok":false,"error":"..."}) returns a Go error to the caller
// but does NOT mark the bridge dead — the subprocess is still healthy
// and serves the next query normally.
func TestQuery_RunnerErrorKeepsBridgeAlive(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_error_then_ok")
	cfg := baseConfig(runnerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(context.Background())

	if _, err := br.Query(ctx, "SELECT * FROM DOESNOTEXIST"); err == nil {
		t.Fatal("expected error on first runner-error response")
	}
	if n := br.RespawnCount(); n != 0 {
		t.Errorf("respawn must not trigger on semantic runner error, got %d", n)
	}
	res, err := br.Query(ctx, "SELECT 1 FROM SYSIBM.SYSDUMMY1")
	if err != nil {
		t.Fatalf("second query: %v", err)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "42" {
		t.Fatalf("unexpected second query result: %#v", res.Rows)
	}
}

// TestQuery_AutoRespawnAfterSubprocessExit exercises the supervision
// path: after the subprocess dies mid-flight, the next Query call must
// respawn the runner and return the expected result from the new
// subprocess.
//
// This is the exact scenario that happened against PUB400 after an
// overnight idle-timeout — without supervision the probe emits only
// health metrics and every collector climbs failure_total.
func TestQuery_AutoRespawnAfterSubprocessExit(t *testing.T) {
	// The fake runner answers one query then exits. The bridge must
	// detect the death (read error on next call) and respawn on the
	// call after that. The respawned process lands in the same
	// behavior (env var is still set) and answers the same canned
	// "first-run" row.
	runnerDir := newFakeRunner(t, "one_query_then_exit")
	cfg := baseConfig(runnerDir)
	cfg.StartupTimeout = 3 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	br, err := New(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close(context.Background())

	res, err := br.Query(ctx, "SELECT 1 FROM SYSIBM.SYSDUMMY1")
	if err != nil {
		t.Fatalf("first query: %v", err)
	}
	if *res.Rows[0][0] != "first-run" {
		t.Fatalf("unexpected first run result: %v", *res.Rows[0][0])
	}

	// Subprocess now exited. The next Query hits EOF on read and
	// marks the bridge dead.
	if _, err := br.Query(ctx, "SELECT 2 FROM SYSIBM.SYSDUMMY1"); err == nil {
		t.Fatal("expected error on second query (subprocess exited)")
	}

	// Third Query: bridge respawns + writes + reads successfully.
	res, err = br.Query(ctx, "SELECT 3 FROM SYSIBM.SYSDUMMY1")
	if err != nil {
		t.Fatalf("third query (after respawn): %v", err)
	}
	if *res.Rows[0][0] != "first-run" {
		t.Fatalf("respawned subprocess gave unexpected result: %v", *res.Rows[0][0])
	}
	if n := br.RespawnCount(); n != 1 {
		t.Errorf("expected respawn_count=1 after recovery, got %d", n)
	}
}

// TestQuery_ClosedBridgeRejectsCalls asserts Close is terminal: once
// called, subsequent Queries return an error immediately rather than
// trying to respawn.
func TestQuery_ClosedBridgeRejectsCalls(t *testing.T) {
	runnerDir := newFakeRunner(t, "ready_exit_on_empty")
	cfg := baseConfig(runnerDir)
	br, err := New(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := br.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := br.Query(context.Background(), "SELECT 1 FROM SYSIBM.SYSDUMMY1"); err == nil {
		t.Fatal("expected error after Close")
	}
}

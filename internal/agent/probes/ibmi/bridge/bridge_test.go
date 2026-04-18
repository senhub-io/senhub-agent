package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newFakeRunner writes a shell-script "java" and a stub runner directory so
// tests can exercise the bridge lifecycle without touching a real JVM.
//
// The returned javaHome satisfies the Config.JavaHome contract: it contains
// a bin/java file which is the fake itself. The fake ignores its arguments
// and produces whatever protocol lines the test asked for via `script`.
func newFakeRunner(t *testing.T, script string) (javaHome, runnerDir string) {
	t.Helper()

	root := t.TempDir()
	javaHome = filepath.Join(root, "javahome")
	runnerDir = filepath.Join(root, "runner")
	if err := os.MkdirAll(filepath.Join(javaHome, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir javahome: %v", err)
	}
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatalf("mkdir runnerDir: %v", err)
	}

	// The fake reads its script from a sibling file so tests can tweak it
	// without re-escaping shell quoting inside Go string literals.
	scriptPath := filepath.Join(root, "script.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	javaBin := filepath.Join(javaHome, "bin", "java")
	content := "#!/bin/sh\nexec /bin/sh " + scriptPath + "\n"
	if err := os.WriteFile(javaBin, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake java: %v", err)
	}
	return javaHome, runnerDir
}

func baseConfig(javaHome, runnerDir string) Config {
	return Config{
		Host:           "example.test",
		User:           "USR",
		Password:       "PWD",
		JavaHome:       javaHome,
		RunnerDir:      runnerDir,
		StartupTimeout: 2 * time.Second,
		QueryTimeout:   2 * time.Second,
	}
}

func TestNew_HandshakeAndQuery(t *testing.T) {
	script := `
echo '{"ok":true,"event":"ready"}'
while read line; do
  if [ -z "$line" ]; then
    exit 0
  fi
  echo '{"ok":true,"columns":["A"],"rows":[["42"]]}'
done
`
	javaHome, runnerDir := newFakeRunner(t, script)
	cfg := baseConfig(javaHome, runnerDir)

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
	script := `echo '{"ok":false,"error":"boom"}'`
	javaHome, runnerDir := newFakeRunner(t, script)
	cfg := baseConfig(javaHome, runnerDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := New(ctx, cfg, nil); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error did not mention runtime error: %v", err)
	}
}

func TestQuery_RejectsEmbeddedNewlines(t *testing.T) {
	script := `
echo '{"ok":true,"event":"ready"}'
read line
`
	javaHome, runnerDir := newFakeRunner(t, script)
	cfg := baseConfig(javaHome, runnerDir)

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
	script := `
echo '{"ok":true,"event":"ready"}'
while read line; do
  if [ -z "$line" ]; then exit 0; fi
  echo '{"ok":false,"error":"SQL0206 column not found"}'
done
`
	javaHome, runnerDir := newFakeRunner(t, script)
	cfg := baseConfig(javaHome, runnerDir)

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
	script := `
echo '{"ok":true,"event":"ready"}'
while read line; do
  if [ -z "$line" ]; then exit 0; fi
done
`
	javaHome, runnerDir := newFakeRunner(t, script)
	cfg := baseConfig(javaHome, runnerDir)

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

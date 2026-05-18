package bridge

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeExecutable creates a file at path with executable bits (0755 on
// Unix; on Windows the .exe extension is what matters, not the mode).
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestResolveRuntime_ExplicitNativeRunnerWins(t *testing.T) {
	dir := t.TempDir()
	native := filepath.Join(dir, "myrunner")
	if runtime.GOOS == "windows" {
		native += ".exe"
	}
	writeExecutable(t, native)

	res := ResolveRuntime(Config{NativeRunner: native})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Mode != RuntimeModeNative {
		t.Errorf("Mode = %q, want native", res.Mode)
	}
	if res.NativeRunner != native {
		t.Errorf("NativeRunner = %q, want %q", res.NativeRunner, native)
	}
	if !strings.HasPrefix(res.Source, "config:") {
		t.Errorf("Source = %q, want prefix 'config:'", res.Source)
	}
}

func TestResolveRuntime_ExplicitNativeRunnerMissing_FallsThrough(t *testing.T) {
	// The native runner is specified but the file doesn't exist —
	// the resolver should record the attempt and try the next
	// candidate. With no other candidate present, it returns Err.
	res := ResolveRuntime(Config{NativeRunner: "/nonexistent/path/jt400runner"})
	if res.Err == nil {
		t.Fatal("expected error when no runtime is available")
	}
	if len(res.Tried) == 0 {
		t.Errorf("Tried list should contain the failed candidate; got empty")
	}
	if !strings.Contains(res.Tried[0], "jt400runner") {
		t.Errorf("Tried[0] = %q, want to mention jt400runner", res.Tried[0])
	}
}

func TestResolveRuntime_ExplicitJavaHomeWithoutRunnerDir_Errors(t *testing.T) {
	// Simulate a JRE present at JavaHome but no runner_dir — the
	// resolver should refuse with a precise error, not crash later
	// in spawn.
	jh := t.TempDir()
	writeExecutable(t, javaBinaryUnder(jh))
	res := ResolveRuntime(Config{JavaHome: jh /* RunnerDir intentionally empty */})
	if res.Err == nil {
		t.Fatal("expected error when JRE found but runner_dir missing")
	}
	if !strings.Contains(res.Err.Error(), "runner_dir") {
		t.Errorf("error should mention runner_dir; got: %v", res.Err)
	}
}

func TestResolveRuntime_JavaHomeFromEnv(t *testing.T) {
	jh := t.TempDir()
	writeExecutable(t, javaBinaryUnder(jh))
	t.Setenv("JAVA_HOME", jh)

	res := ResolveRuntime(Config{RunnerDir: jh /* any non-empty dir for JRE mode */})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Mode != RuntimeModeJava {
		t.Errorf("Mode = %q, want java", res.Mode)
	}
	if res.Source != "env: JAVA_HOME" {
		t.Errorf("Source = %q, want 'env: JAVA_HOME'", res.Source)
	}
}

func TestResolveRuntime_NothingFound_ListsCandidates(t *testing.T) {
	t.Setenv("JAVA_HOME", "") // make sure the env path doesn't accidentally resolve
	res := ResolveRuntime(Config{
		NativeRunner: "/nonexistent/run",
		JavaHome:     "/nonexistent/jdk",
		RunnerDir:    "/somewhere",
	})
	if res.Err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"no IBM i runtime found", "Paths tried", "/nonexistent/run", "/nonexistent/jdk"} {
		if !strings.Contains(res.Err.Error(), want) {
			t.Errorf("error missing %q; full error:\n%v", want, res.Err)
		}
	}
	if !strings.Contains(res.Err.Error(), "ibmi check") {
		t.Errorf("error should point to the diagnostic command; got: %v", res.Err)
	}
}

func TestResolveRuntime_ConfigNativeBeatsAutoDetect(t *testing.T) {
	// When the operator pins NativeRunner explicitly, the resolver
	// must use it even if a sibling binary would also be present.
	// We can't easily plant a real sibling next to os.Executable()
	// without polluting the test binary's directory; but verifying
	// the priority is observable by checking Source.
	dir := t.TempDir()
	native := filepath.Join(dir, "explicit-runner")
	if runtime.GOOS == "windows" {
		native += ".exe"
	}
	writeExecutable(t, native)

	res := ResolveRuntime(Config{NativeRunner: native})
	if res.Source != "config: bridge.native_runner" {
		t.Errorf("Source = %q, want 'config: bridge.native_runner'", res.Source)
	}
}

func TestSiblingNativeRunner_ShapeIsPlatformAware(t *testing.T) {
	got := siblingNativeRunner()
	if got == "" {
		t.Skip("os.Executable() unavailable in this test environment")
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(got, "\\bridge\\jt400runner.exe") && !strings.HasSuffix(got, "/bridge/jt400runner.exe") {
			t.Errorf("got %q, want it to end with bridge/jt400runner.exe", got)
		}
	} else {
		if !strings.HasSuffix(got, "/bridge/jt400runner") {
			t.Errorf("got %q, want it to end with /bridge/jt400runner", got)
		}
	}
}

func TestJavaBinaryUnder_PlatformAware(t *testing.T) {
	got := javaBinaryUnder("/opt/jdk-17")
	wantSuffix := "/bin/java"
	if runtime.GOOS == "windows" {
		wantSuffix = "\\bin\\java.exe"
		// On Windows filepath.Join uses backslash but we accept either.
		if !strings.HasSuffix(got, wantSuffix) && !strings.HasSuffix(got, "/bin/java.exe") {
			t.Errorf("got %q, want suffix bin/java.exe", got)
		}
	} else {
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("got %q, want suffix %q", got, wantSuffix)
		}
	}
}

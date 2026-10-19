package execprobe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseConfig_SuccessCases(t *testing.T) {
	absCheck := filepath.Join(t.TempDir(), "check")

	cases := []struct {
		name   string
		config map[string]interface{}
		want   execConfig
	}{
		{
			name:   "defaults",
			config: map[string]interface{}{"command": absCheck},
			want: execConfig{
				Command:  absCheck,
				Format:   "nagios",
				Interval: 60 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
		{
			name: "interface args json env workdir interval timeout",
			config: map[string]interface{}{
				"command":  absCheck,
				"args":     []interface{}{"--flag", "value"},
				"format":   "json",
				"interval": 30,
				"timeout":  5,
				"workdir":  "/tmp",
				"env":      map[string]interface{}{"A": "B"},
			},
			want: execConfig{
				Command:  absCheck,
				Args:     []string{"--flag", "value"},
				Format:   "json",
				Interval: 30 * time.Second,
				Timeout:  5 * time.Second,
				WorkDir:  "/tmp",
				Env:      map[string]string{"A": "B"},
			},
		},
		{
			name: "string args and nagios accepted",
			config: map[string]interface{}{
				"command": absCheck,
				"args":    []string{"x", "y"},
				"format":  "nagios",
			},
			want: execConfig{
				Command:  absCheck,
				Args:     []string{"x", "y"},
				Format:   "nagios",
				Interval: 60 * time.Second,
				Timeout:  10 * time.Second,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConfig(tc.config)
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseConfig() got = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseConfig_NonPositiveIntervalAndTimeoutKeepDefaults(t *testing.T) {
	absCheck := filepath.Join(t.TempDir(), "check")

	cases := []struct {
		name     string
		interval interface{}
		timeout  interface{}
	}{
		{name: "zero values", interval: 0, timeout: 0},
		{name: "negative values", interval: -5, timeout: -2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig(map[string]interface{}{
				"command":  absCheck,
				"interval": tc.interval,
				"timeout":  tc.timeout,
			})
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if cfg.Interval != 60*time.Second {
				t.Errorf("Interval = %v, want default 60s", cfg.Interval)
			}
			if cfg.Timeout != 10*time.Second {
				t.Errorf("Timeout = %v, want default 10s", cfg.Timeout)
			}
		})
	}
}

func TestParseConfig_RejectsNonStringArgOrEnvValue(t *testing.T) {
	absCheck := filepath.Join(t.TempDir(), "check")

	cases := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{
			name:   "non-string arg",
			config: map[string]interface{}{"command": absCheck, "args": []interface{}{"ok", 42}},
			want:   "exec args must be strings (got int)",
		},
		{
			name:   "non-string env value",
			config: map[string]interface{}{"command": absCheck, "env": map[string]interface{}{"TOKEN": true}},
			want:   "exec env values must be strings (got bool for TOKEN)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.config)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseConfig_ShellCommandStringIsStoredLiteral(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX absolute-path fixture")
	}

	cmd := "/bin/sh -c 'echo pwned'"
	cfg, err := parseConfig(map[string]interface{}{"command": cmd})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Command != cmd {
		t.Errorf("Command = %q, want %q; parseConfig must not shell-split", cfg.Command, cmd)
	}
}

func TestNewExecProbe_RejectsShellCommandStringAtExecutableCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX absolute-path fixture")
	}

	cmd := "/bin/sh -c 'echo pwned'"
	if _, err := NewExecProbe(map[string]interface{}{"command": cmd}, testBaseLogger()); err == nil {
		t.Fatal("expected construction to reject a shell command string as an executable path")
	}
}

func TestBuildEnv_ExtendsInheritedEnvWithConfiguredOverrides(t *testing.T) {
	t.Setenv("SENHUB_EXECPROBE_INHERIT", "parent-inherit")
	t.Setenv("SENHUB_EXECPROBE_OVERRIDE", "parent-override")

	got := buildEnv(map[string]string{
		"SENHUB_EXECPROBE_OVERRIDE":  "child-override",
		"SENHUB_EXECPROBE_EXTRA":     "configured-extra",
		"SENHUB_EXECPROBE_SECRETISH": "configured-secret",
	})

	if v, ok := lastEnvValueForTest(got, "SENHUB_EXECPROBE_INHERIT"); !ok || v != "parent-inherit" {
		t.Errorf("inherited-only env: got (%q, %v), want (\"parent-inherit\", true)", v, ok)
	}
	if v, ok := lastEnvValueForTest(got, "SENHUB_EXECPROBE_OVERRIDE"); !ok || v != "child-override" {
		t.Errorf("overridden env last value: got (%q, %v), want (\"child-override\", true)", v, ok)
	}
	if v, ok := lastEnvValueForTest(got, "SENHUB_EXECPROBE_EXTRA"); !ok || v != "configured-extra" {
		t.Errorf("configured extra env: got (%q, %v), want (\"configured-extra\", true)", v, ok)
	}
	if v, ok := lastEnvValueForTest(got, "SENHUB_EXECPROBE_SECRETISH"); !ok || v != "configured-secret" {
		t.Errorf("configured sensitive-looking env must pass through unchanged: got (%q, %v)", v, ok)
	}
}

func TestExecProbe_LifecycleHooksAndStrategies(t *testing.T) {
	script := writeScript(t, "exit 0\n", "@exit /b 0\r\n")
	p := newTestProbe(t, map[string]interface{}{"command": script, "interval": 30})

	wantStrategies := []string{"senhub", "prtg", "http", "otlp"}
	if got := p.GetTargetStrategies(); !reflect.DeepEqual(got, wantStrategies) {
		t.Errorf("GetTargetStrategies() = %v, want %v", got, wantStrategies)
	}
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
	if got := p.GetInterval(); got != 30*time.Second {
		t.Errorf("GetInterval() = %v, want 30s", got)
	}
	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Errorf("OnStart() error = %v, want nil", err)
	}
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown() error = %v, want nil", err)
	}
}

func TestRunOnce_KillsWholeProcessGroupOnTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group semantics")
	}

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("SENHUB_EXECPROBE_CHILD_PID_FILE", pidFile)
	script := writeScript(t,
		`child_pid_file="$SENHUB_EXECPROBE_CHILD_PID_FILE"
( sleep 30 ) &
child=$!
echo $child > "$child_pid_file"
wait $child`,
		``,
	)

	p := newTestProbe(t, map[string]interface{}{"command": script, "timeout": 1})
	done := make(chan execResult, 1)
	go func() { done <- p.run() }()

	childPid := waitForChildPidFileForTest(t, pidFile)

	var res execResult
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("p.run() did not return after timeout kill")
	}
	if !res.timedOut {
		t.Fatalf("run timedOut = false, want true (result: %+v)", res)
	}

	deadline := time.Now().Add(5 * time.Second)
	for unixProcessExistsForTest(childPid) {
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived process-group kill", childPid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func lastEnvValueForTest(env []string, key string) (string, bool) {
	prefix := key + "="
	value := ""
	found := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			value = strings.TrimPrefix(item, prefix)
			found = true
		}
	}
	return value, found
}

func waitForChildPidFileForTest(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("child PID file not created before timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func unixProcessExistsForTest(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

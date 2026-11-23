package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"senhub-agent.go/internal/cliexit"
)

func TestConfirmationsAtATerminal(t *testing.T) {
	prompts := []struct {
		name    string
		confirm func(answer string) (proceed bool, code int, out string)
	}{
		{"uninstall", func(answer string) (bool, int, string) {
			var out, errOut bytes.Buffer
			proceed, code := confirmUninstallFrom(strings.NewReader(answer), true, &out, &errOut)
			return proceed, code, out.String() + errOut.String()
		}},
		{"license remove", func(answer string) (bool, int, string) {
			var out, errOut bytes.Buffer
			proceed, code := confirmLicenseRemoveFrom(strings.NewReader(answer), true, &out, &errOut)
			return proceed, code, out.String() + errOut.String()
		}},
	}
	for _, p := range prompts {
		t.Run(p.name+"/y proceeds", func(t *testing.T) {
			proceed, code, _ := p.confirm("y\n")
			if !proceed || code != cliexit.OK {
				t.Errorf("proceed=%v code=%d, want true, OK", proceed, code)
			}
		})
		for _, answer := range []string{"n\n", "\n", ""} {
			t.Run(p.name+"/"+strings.TrimSpace(answer)+" cancels with exit 0", func(t *testing.T) {
				proceed, code, out := p.confirm(answer)
				if proceed || code != cliexit.OK {
					t.Errorf("proceed=%v code=%d, want false, OK", proceed, code)
				}
				if !strings.Contains(strings.ToLower(out), "cancel") {
					t.Errorf("output %q does not say the command was cancelled", out)
				}
			})
		}
	}
}

// A refusal without a terminal must reach the process exit code: reverting
// the wiring in handleServiceCommand or handleLicenseRemove to a bare
// return would exit 0 and still pass the direct calls to the confirm
// helpers.
func TestConfirmationRefusalExitsNonZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("subprocess re-exec test skipped on windows")
	}
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	if os.Getenv("SENHUB_CONFIRM_HELPER") == "1" {
		os.Args = strings.Split(os.Getenv("SENHUB_CONFIRM_ARGS"), "\x1f")
		Main()
		os.Exit(0)
	}
	home := t.TempDir()
	cfg := filepath.Join(home, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("agent: {}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"uninstall", []string{"agent", "uninstall", "--config-path", cfg}, "--yes"},
		{"license remove", []string{"agent", "license", "remove", "--config-path", cfg}, "--force"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestConfirmationRefusalExitsNonZero$")
			cmd.Dir = home
			cmd.Env = append(os.Environ(),
				"SENHUB_CONFIRM_HELPER=1",
				"SENHUB_CONFIRM_ARGS="+strings.Join(tc.args, "\x1f"),
				"HOME="+home, "XDG_CONFIG_HOME="+home, "XDG_STATE_HOME="+home, "XDG_DATA_HOME="+home,
			)
			cmd.Stdin = strings.NewReader("y\n")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != cliexit.Failure {
				t.Fatalf("err = %v, want exit code %d\nstdout: %s\nstderr: %s", err, cliexit.Failure, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr %q does not name %s", stderr.String(), tc.want)
			}
			if _, statErr := os.Stat(cfg); statErr != nil {
				t.Errorf("the configuration was touched: %v", statErr)
			}
		})
	}
}

package app

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestHelpRequested(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"wire-unit", "-h"}, true},
		{[]string{"rm", "x", "--help"}, true},
		{[]string{"--", "-h"}, false},
		{[]string{"set", "name"}, false},
		{[]string{"-help"}, false},
	}
	for _, tt := range tests {
		if got := helpRequested(tt.args); got != tt.want {
			t.Errorf("helpRequested(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestInterceptHelp(t *testing.T) {
	tests := []struct {
		args      []string
		want      bool
		wantUsage bool
	}{
		{[]string{"agent", "secret", "wire-unit", "-h"}, true, true},
		{[]string{"agent", "secret", "migrate", "--help"}, true, true},
		{[]string{"agent", "secret", "rm", "x", "-h"}, true, true},
		{[]string{"agent", "secret", "wire-unit", "--", "-h"}, false, false},
		{[]string{"agent", "key", "show", "-h"}, true, true},
		{[]string{"agent", "zabbix", "setup", "-h"}, true, true},
		{[]string{"agent", "license", "activate", "-h"}, false, false},
		{[]string{"agent", "update", "-h"}, false, false},
		{[]string{"agent", "install", "-h"}, false, false},
		{[]string{"agent", "restart", "--help"}, false, false},
		{[]string{"agent", "refresh-unit", "-h"}, false, false},
		{[]string{"agent", "secret", "wire-unit"}, false, false},
		{[]string{"agent", "secret"}, false, false},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		got := interceptHelp(tt.args, &buf)
		if got != tt.want {
			t.Errorf("interceptHelp(%v) = %v, want %v", tt.args, got, tt.want)
			continue
		}
		if tt.wantUsage && (buf.Len() == 0 || !strings.Contains(buf.String(), "Usage")) {
			t.Errorf("interceptHelp(%v) wrote %q, want non-empty usage", tt.args, buf.String())
		}
	}
}

func TestInterceptHelpExtraCommandUsage(t *testing.T) {
	name := "test-usage-extra"
	extraCommands[name] = ExtraCommand{
		Name: name,
		Usage: func(w io.Writer) {
			io.WriteString(w, "Usage: agent test-usage-extra do\n")
		},
		Run: func() {
			t.Fatal("extra command ran despite -h")
		},
	}
	t.Cleanup(func() { delete(extraCommands, name) })

	var buf bytes.Buffer
	if !interceptHelp([]string{"agent", name, "do", "-h"}, &buf) {
		t.Fatalf("interceptHelp for registered extra command = false, want true")
	}
	if !strings.Contains(buf.String(), "Usage: agent test-usage-extra do") {
		t.Errorf("usage = %q, want extra command usage text", buf.String())
	}
}

func TestReadOnlyCommandHelp(t *testing.T) {
	if !readOnlyCommand([]string{"agent", "secret", "wire-unit", "-h"}) {
		t.Error("readOnlyCommand with -h = false, want true")
	}
	if readOnlyCommand([]string{"agent", "secret", "wire-unit"}) {
		t.Error("readOnlyCommand without -h = true, want false")
	}
	if readOnlyCommand([]string{"agent", "secret", "wire-unit", "--", "-h"}) {
		t.Error("readOnlyCommand with -- -h = true, want false")
	}
	if readOnlyCommand([]string{"agent", "install", "-h"}) {
		t.Error("readOnlyCommand install -h = true, want false")
	}
	if readOnlyCommand([]string{"agent", "restart", "--help"}) {
		t.Error("readOnlyCommand restart --help = true, want false")
	}
}

func TestEveryCommandHasHelpGuard(t *testing.T) {
	intercepted := map[string]struct{}{
		"config":        {},
		"db-monitoring": {},
		"console":       {},
		"doctor":        {},
		"secret":        {},
		"key":           {},
		"zabbix":        {},
	}
	check := func(name string) {
		if _, ok := selfHelpCommands[name]; ok {
			return
		}
		if _, ok := intercepted[name]; !ok {
			t.Errorf("command %q is neither self-help nor in the intercepted set; add it to one of them", name)
		}
	}
	for name := range extraCommands {
		check(name)
	}
	for name := range knownTopLevelArgs {
		if strings.HasPrefix(name, "-") || name == "help" || name == "version" || name == "debug-modules-list" {
			continue
		}
		check(name)
	}
}

func TestMainHelpDoesNotAct(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("subprocess re-exec test skipped on windows")
	}
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	if os.Getenv("SENHUB_HELP_HELPER") == "1" {
		os.Args = strings.Split(os.Getenv("SENHUB_HELP_ARGS"), "\x1f")
		Main()
		os.Exit(0)
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"agent", "key", "show", "-h", "--config-path", "/nonexistent/agent.yaml"}, "Usage"},
		{[]string{"agent", "secret", "list", "--help", "--config-path", "/nonexistent/agent.yaml"}, "Usage"},
		{[]string{"agent", "config", "migrate", "-h"}, "SenHub Agent"},
		{[]string{"agent", "license", "show", "-h"}, "sage"},
	}
	for _, tt := range cases {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelpDoesNotAct$")
		cmd.Env = append(os.Environ(),
			"SENHUB_HELP_HELPER=1",
			"SENHUB_HELP_ARGS="+strings.Join(tt.args, "\x1f"),
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Errorf("args %v: exit error %v\nstdout: %s\nstderr: %s", tt.args, err, stdout.String(), stderr.String())
			continue
		}
		out := stdout.String()
		if tt.want == "sage" {
			if !strings.Contains(out, "Usage") && !strings.Contains(out, "usage") {
				t.Errorf("args %v: stdout %q missing usage text", tt.args, out)
			}
		} else if !strings.Contains(out, tt.want) {
			t.Errorf("args %v: stdout %q missing %q", tt.args, out, tt.want)
		}
	}
}

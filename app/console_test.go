package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConsoleArgs(t *testing.T) {
	opts, err := parseConsoleArgs([]string{"--print", "--config-path", "/etc/senhub/agent.yaml", "--handoff", "/tmp/h"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.print || opts.configPath != "/etc/senhub/agent.yaml" || opts.handoff != "/tmp/h" {
		t.Errorf("parsed = %+v", opts)
	}
	if _, err := parseConsoleArgs([]string{"--config-path"}); err == nil {
		t.Error("a dangling --config-path must be rejected")
	}
	if _, err := parseConsoleArgs([]string{"--open"}); err == nil {
		t.Error("an unknown flag must be rejected")
	}
}

func TestConsoleURL_MultiFile(t *testing.T) {
	// The shortcut and the finish page of the installer rely on this
	// address being the one that answers: key from agent.yaml, port and
	// scheme from strategies.d/.
	// The key reader accepts the installed configuration or a file under
	// the working directory, as `status` does.
	dir := t.TempDir()
	t.Chdir(dir)
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"0b6b1e2a-8f4e-4c3a-9c2d-1f2e3d4c5b6a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"),
		[]byte("http:\n  port: 9080\n  endpoints: [\"web\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := consoleURL(main)
	if err != nil {
		t.Fatalf("consoleURL: %v", err)
	}
	if want := "http://localhost:9080/web/0b6b1e2a-8f4e-4c3a-9c2d-1f2e3d4c5b6a/dashboard"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if _, err := consoleURL(filepath.Join(dir, "absent.yaml")); err == nil || !strings.Contains(err.Error(), "agent key") {
		t.Errorf("a missing config must be reported as an unreadable key, got: %v", err)
	}
}

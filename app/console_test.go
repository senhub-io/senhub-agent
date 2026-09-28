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
	// address being the one that answers: the ADMINISTRATION key from
	// the http output, port and scheme from strategies.d/. The agent
	// key is what a monitoring tool reads with and does not open the
	// console.
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
		[]byte("http:\n  port: 9080\n  endpoints: [\"web\"]\n  admin_key: \"11112222-3333-4444-5555-666677778888\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := consoleURL(main)
	if err != nil {
		t.Fatalf("consoleURL: %v", err)
	}
	if want := "http://localhost:9080/web/11112222-3333-4444-5555-666677778888/dashboard"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
	if strings.Contains(url, "0b6b1e2a") {
		t.Errorf("the console address carries the agent key: %s", url)
	}
	if _, err := consoleURL(filepath.Join(dir, "absent.yaml")); err == nil {
		t.Error("a missing config must be reported rather than yielding an address")
	}
}

// An installation whose http output has no administration key serves no
// console. Saying so is what keeps the shortcut from opening an address
// that answers 404, which an operator reads as a broken agent.
func TestConsoleURLSaysSoWhenNoAdministrationKeyExists(t *testing.T) {
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
	_, err := consoleURL(main)
	if err == nil {
		t.Fatal("an installation with no administration key must not yield a console address")
	}
	if !strings.Contains(err.Error(), "administration key") {
		t.Errorf("the error must name what is missing, got: %v", err)
	}
}

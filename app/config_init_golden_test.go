package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const configInitGoldenPath = "testdata/config_init.golden"

func TestRunConfigInitGoldenOutput(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "no-license")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	env := testInitEnv()
	env.checkPort = func(bind string, port int) error {
		if port == 9999 {
			return errors.New("port 9999 is already in use on " + bind)
		}
		return nil
	}

	cfg := func(name string) string { return filepath.Join(dir, name, "agent.yaml") }
	steps := []struct {
		name string
		argv []string
	}{
		{"fresh", []string{"--config-path", cfg("a"), "--otlp-endpoint", "otlp.example.com:4317", "--otlp-protocol", "http",
			"--tags", "env=test,site=lab", "--zabbix-server", "zbx.example.com", "--zabbix-host-metadata", "meta", "--http-bind", "127.0.0.1", "--http-port", "18081",
			"--license-dir", empty}},
		{"again-same-args", []string{"--config-path", cfg("a"), "--otlp-endpoint", "otlp.example.com:4317", "--otlp-protocol", "http", "--zabbix-server", "zbx.example.com"}},
		{"again-other-port", []string{"--config-path", cfg("a"), "--http-bind", "127.0.0.2", "--http-port", "1234"}},
		{"again-new-fragments", []string{"--config-path", cfg("a"), "--otlp-endpoint", "other.example.com:4317", "--zabbix-server", "zbx2.example.com"}},
		{"fresh-json", []string{"--config-path", cfg("b"), "--json", "--tags", "k=v"}},
		{"again-json", []string{"--config-path", cfg("b"), "--json"}},
		{"ok-if-unchanged", []string{"--config-path", cfg("b"), "--ok-if-unchanged"}},
		{"fresh-bare", []string{"--config-path", cfg("f")}},
		{"late-fragments", []string{"--config-path", cfg("f"), "--otlp-endpoint", "late.example.com:4317", "--zabbix-server", "zbx3.example.com", "--json"}},
		{"busy-port", []string{"--config-path", cfg("c"), "--http-port", "9999"}},
		{"bad-flag", []string{"--config-path", cfg("d"), "--nonsense"}},
		{"bad-flag-json", []string{"--config-path", cfg("d"), "--nonsense", "--json"}},
		{"bad-protocol", []string{"--config-path", cfg("d"), "--otlp-protocol", "carrier-pigeon", "--otlp-endpoint", "x:1"}},
		{"unreadable-license-file", []string{"--config-path", cfg("e"), "--license-file", filepath.Join(dir, "missing.jwt")}},
		{"empty-path", []string{"--config-path", ""}},
	}

	var sb strings.Builder
	for _, s := range steps {
		var out bytes.Buffer
		code := runConfigInit(s.argv, &out, env)
		text := strings.ReplaceAll(out.String(), dir, "<dir>")
		text = strings.ReplaceAll(text, "/private<dir>", "<dir>")
		fmt.Fprintf(&sb, "===== %s (exit %d) =====\n%s", s.name, code, text)
		if !strings.HasSuffix(text, "\n") {
			sb.WriteString("\n")
		}
	}

	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sb, "===== file %s =====\n%s", rel, strings.ReplaceAll(string(body), dir, "<dir>"))
	}
	got := sb.String()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(configInitGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configInitGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(configInitGoldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("runConfigInit output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

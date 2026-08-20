package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHTTPStrategyPort(t *testing.T) {
	// A multi-file host keeps the http strategy in strategies.d/, where a
	// `strategies:` lookup in the main file finds nothing — status then
	// probed 8080, missed the daemon and fell back to the degraded local
	// view, hiding the dead-output report (#826).
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"),
		[]byte("http:\n  port: 19100\n  endpoints: [\"prtg\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := resolveHTTPStrategyPort(main); got != 19100 {
		t.Errorf("port=%d, want 19100 (read from strategies.d)", got)
	}
	if got := resolveHTTPStrategyPort(filepath.Join(dir, "absent.yaml")); got != 8080 {
		t.Errorf("port=%d for a missing config, want the 8080 default", got)
	}
}

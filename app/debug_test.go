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

func TestBuildDashboardURL_MultiFileWithTLS(t *testing.T) {
	// The printed console address must be the one that answers: the
	// port and scheme come from strategies.d/, not from a constant.
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"),
		[]byte("http:\n  port: 9443\n  endpoints: [\"web\"]\n  tls:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := buildDashboardURL(main, "k"), "https://localhost:9443/web/k/dashboard"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if got, want := buildDashboardURL(filepath.Join(dir, "absent.yaml"), "k"), "http://localhost:8080/web/k/dashboard"; got != want {
		t.Errorf("url for a missing config = %q, want %q", got, want)
	}
	if got := buildDashboardURL(main, ""); got != "" {
		t.Errorf("url without an agent key = %q, want empty", got)
	}
}

func TestResolveHTTPStrategyListen_BindAndLocalHost(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"),
		[]byte("http:\n  port: 19100\n  bind_address: \"10.0.0.5\"\n  endpoints: [\"prtg\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scheme, bind, port := resolveHTTPStrategyListen(main)
	if scheme != "http" || bind != "10.0.0.5" || port != 19100 {
		t.Errorf("got %s %s %d", scheme, bind, port)
	}
	// An output bound to one interface does not answer on localhost.
	if got := localHostFor(bind); got != "10.0.0.5" {
		t.Errorf("localHostFor(10.0.0.5) = %s", got)
	}
	for _, any := range []string{"", "0.0.0.0", "::"} {
		if got := localHostFor(any); got != "127.0.0.1" {
			t.Errorf("localHostFor(%q) = %s, want loopback", any, got)
		}
	}
}

func TestReportSchemaProblems_UnreadParamIsAWarning(t *testing.T) {
	errs, warns := reportSchemaProblems("cpu", "cpu", map[string]interface{}{"no_such_param": 1})
	if errs != 0 || warns != 1 {
		t.Errorf("an unread parameter the agent ignores must be a warning, got %d errors %d warnings", errs, warns)
	}
}

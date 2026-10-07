package configuration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

const monolithicHTTPConfig = `config_version: 2
agent:
  key: "agent-key"
probes: []
storage:
  - name: http
    params:
      port: 8080
      bind_address: 127.0.0.1
      endpoints: [prtg, web, nagios]
`

func httpFragmentText(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "strategies.d", "*-http.yaml"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("want one http fragment, got %v (err %v)", matches, err)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func startOnce(t *testing.T, cfg string) {
	t.Helper()
	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: cfg}, createTestLocalLogger())
	if err := lc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = lc.Shutdown(context.Background())
}

// A monolithic file has its http output in a `storage:` sequence, where the
// key could not be added; the split must come first so the key is minted in
// the same boot and survives into the fragment.
func TestMonolithicConfigGetsAnAdminKeyInTheSameBoot(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte(monolithicHTTPConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	startOnce(t, cfg)
	first := httpFragmentText(t, dir)
	if !strings.Contains(first, "admin_key:") {
		t.Fatalf("the http fragment has no admin_key after one start:\n%s", first)
	}

	startOnce(t, cfg)
	if second := httpFragmentText(t, dir); second != first {
		t.Errorf("a second start changed the fragment:\n--- first\n%s\n--- second\n%s", first, second)
	}
}

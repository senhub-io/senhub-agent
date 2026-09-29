package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInstall lays out a multi-file installation with the given http
// fragment, the way an agent on disk looks.
func writeInstall(t *testing.T, fragment string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 3\nagent:\n  key: \"agent-key\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"), []byte(fragment), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func httpFragment(t *testing.T, configPath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "strategies.d", "00-http.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// An installation made before the two surfaces were told apart has no
// administration key, so its console — the one its desktop shortcut
// opens — would not be served at all. It is given one on the first
// start that finds none.
func TestAnInstallationWithoutAnAdministrationKeyIsGivenOne(t *testing.T) {
	cfg := writeInstall(t, "http:\n  port: 8080\n  endpoints: [\"prtg\", \"web\"]\n")

	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatalf("EnsureAdminKey: %v", err)
	}

	out := httpFragment(t, cfg)
	if !strings.Contains(out, "admin_key:") {
		t.Fatalf("no administration key was written:\n%s", out)
	}
	// What was there stays there: the migration adds, it does not rewrite.
	for _, kept := range []string{"port: 8080", "prtg", "web"} {
		if !strings.Contains(out, kept) {
			t.Errorf("the fragment lost %q:\n%s", kept, out)
		}
	}
	// The key must not be the agent key: telling them apart is the point.
	if strings.Contains(out, "agent-key") {
		t.Errorf("the agent key was reused as the administration key:\n%s", out)
	}
}

// Run twice, the second start must change nothing: a new key each time
// would invalidate the console address an operator just bookmarked.
func TestGivingTheKeyIsIdempotent(t *testing.T) {
	cfg := writeInstall(t, "http:\n  port: 8080\n  endpoints: [\"web\"]\n")

	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatal(err)
	}
	first := httpFragment(t, cfg)
	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if second := httpFragment(t, cfg); second != first {
		t.Errorf("the second start changed the fragment:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// A sealed installation holds a reference, not a value. It has a key.
func TestASealedKeyIsLeftAlone(t *testing.T) {
	cfg := writeInstall(t, "http:\n  port: 8080\n  admin_key: \"${secret:http.admin_key}\"\n")
	before := httpFragment(t, cfg)

	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if after := httpFragment(t, cfg); after != before {
		t.Errorf("a sealed key was overwritten:\n%s", after)
	}
}

// An installation with no http output has no console to open and needs
// no key; it must not be rewritten, and must not fail the start.
func TestAnInstallationWithoutTheHTTPOutputIsUntouched(t *testing.T) {
	cfg := writeInstall(t, "otlp:\n  endpoint: \"collector:4317\"\n")
	before := httpFragment(t, cfg)

	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatalf("a configuration with no http output must not fail: %v", err)
	}
	if after := httpFragment(t, cfg); after != before {
		t.Errorf("an unrelated fragment was rewritten:\n%s", after)
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

// A real, registered probe schema drives the typing: the port becomes a
// number, and a value its schema refuses stops the load by variable name.
func TestEnvProbes_AgainstARegisteredSchema(t *testing.T) {
	wireProbeTypeLookup()
	t.Cleanup(func() { configuration.SetProbeTypeLookup(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENHUB_PROBE_CACHE_TYPE", "redis")
	t.Setenv("SENHUB_PROBE_CACHE_HOST", "redis.internal")
	t.Setenv("SENHUB_PROBE_CACHE_PORT", "6380")
	t.Setenv("SENHUB_PROBE_CACHE_TLS", "true")
	t.Setenv("SENHUB_PROBE_CACHE_TLS_CA_FILE", "/etc/ca.pem")

	data, err := configuration.LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Probes) != 1 {
		t.Fatalf("probes = %+v", data.Probes)
	}
	p := data.Probes[0].Params
	if data.Probes[0].Name != "cache" || p["port"] != 6380 || p["tls"] != true || p["tls_ca_file"] != "/etc/ca.pem" {
		t.Fatalf("probe = %+v", data.Probes[0])
	}

	t.Setenv("SENHUB_PROBE_CACHE_PORT", "high")
	if _, err := configuration.LoadFromDisk(cfg, nil); err == nil || !strings.Contains(err.Error(), "SENHUB_PROBE_CACHE_PORT") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("SENHUB_PROBE_CACHE_PORT", "6380")
	t.Setenv("SENHUB_PROBE_CACHE_TYPE", "nosuchprobe")
	if _, err := configuration.LoadFromDisk(cfg, nil); err == nil || !strings.Contains(err.Error(), "nosuchprobe") {
		t.Fatalf("unknown type: %v", err)
	}
}

// config show names the variables, never their values.
func TestConfigShow_NamesEnvProbeVariables(t *testing.T) {
	wireProbeTypeLookup()
	t.Cleanup(func() { configuration.SetProbeTypeLookup(nil) })
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENHUB_PROBE_CACHE_TYPE", "redis")
	t.Setenv("SENHUB_PROBE_CACHE_PASSWORD", "hunter2")

	var out strings.Builder
	if code := runConfigShow([]string{cfg}, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "# Probes read from the environment") || !strings.Contains(got, "SENHUB_PROBE_CACHE_PASSWORD") {
		t.Fatalf("no provenance line:\n%s", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Fatalf("the default show prints the secret:\n%s", got)
	}
}

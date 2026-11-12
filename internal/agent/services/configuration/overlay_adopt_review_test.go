package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/extension"
)

func TestReportMasksStructuredSecretValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	agentYAML := "config_version: 3\nagent:\n  name: t\n"
	probeYAML := `- name: db
  type: postgresql
  params:
    password:
      value: hunter2
    auth:
      user: u
      pass: inner-pw
    conn_opts: ["${env:ADOPT_R1:-dflt-pw}"]
    note: ${foo:bar}
`
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agentYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-db.yaml"), []byte(probeYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{
					Name:   "db",
					Type:   "postgresql",
					Params: ProbeConfigParams{"host": "db1"},
				},
			},
		},
		configPath: filepath.Join(dir, "agent.yaml"),
	}
	probes := target.Probes()
	if len(probes) != 1 || probes[0].Name != "db" {
		t.Fatalf("expected one reported probe named db, got %#v", probes)
	}
	b, err := json.Marshal(probes[0].Params)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, secret := range []string{"hunter2", "inner-pw", "dflt-pw"} {
		if strings.Contains(s, secret) {
			t.Errorf("reported params contain secret %q: %s", secret, s)
		}
	}
	if probes[0].Params["password"] != "***" {
		t.Errorf("password = %#v, want *** (the whole value)", probes[0].Params["password"])
	}
	if probes[0].Params["note"] != "***" {
		t.Errorf("note = %#v, want *** (unknown scheme)", probes[0].Params["note"])
	}
	secretParams := strings.Join(probes[0].SecretParams, ",")
	for _, want := range []string{"password", "auth", "conn_opts"} {
		if !strings.Contains(secretParams, want) {
			t.Errorf("SecretParams %v missing %q", probes[0].SecretParams, want)
		}
	}
}

func TestAdoptRefusesRetargetWhenTheFileCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	enabled := true
	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{
					Name:    "db",
					Type:    "postgresql",
					Enabled: &enabled,
					Params:  ProbeConfigParams{"host": "db1", "password": "x"},
				},
			},
		},
		configPath: filepath.Join(dir, "does-not-exist.yaml"),
	}
	if target.AdoptProbe(extension.ManagedProbe{
		Name:   "db",
		Type:   "postgresql",
		Params: map[string]any{"host": "evil"},
	}) {
		t.Error("AdoptProbe with a target param succeeded although the config file cannot be read")
	}
	if !target.AdoptProbe(extension.ManagedProbe{
		Name:   "db",
		Type:   "postgresql",
		Params: map[string]any{"sslmode": "require"},
	}) {
		t.Error("AdoptProbe without a target param was refused although the file cannot be read")
	}
}

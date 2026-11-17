package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/extension"
)

func writeSecurityTestConfig(t *testing.T, probeYAML string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	agentYAML := "config_version: 3\nagent:\n  name: t\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agentYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-db.yaml"), []byte(probeYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "agent.yaml")
}

func TestOverlayReportMasksDefaultsAndScalars(t *testing.T) {
	probeYAML := `- name: db
  type: postgresql
  params:
    password: 1234
    token: ${env:ADOPT_T2:-default-s3cret}
    enabled_flag: true
`
	configPath := writeSecurityTestConfig(t, probeYAML)
	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{Name: "db", Type: "postgresql", Params: ProbeConfigParams{"host": "db1"}},
			},
		},
		configPath: configPath,
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
	for _, secret := range []string{"1234", "default-s3cret"} {
		if strings.Contains(s, secret) {
			t.Errorf("reported params contain secret %q: %s", secret, s)
		}
	}
	if probes[0].Params["password"] != "***" {
		t.Errorf("password = %#v, want \"***\"", probes[0].Params["password"])
	}
	if probes[0].Params["enabled_flag"] != true {
		t.Errorf("enabled_flag = %#v, want true", probes[0].Params["enabled_flag"])
	}
}

func TestOverlayAdoptRefusesRetargetWithLocalSecret(t *testing.T) {
	probeYAML := `- name: db
  type: postgresql
  params:
    host: db1
    password: ${secret:db.pw}
`
	configPath := writeSecurityTestConfig(t, probeYAML)
	newTarget := func() *overlayTarget {
		enabled := true
		return &overlayTarget{
			cfg: &LocalConfigurationData{
				Probes: []ProbeConfig{
					{
						Name:    "db",
						Type:    "postgresql",
						Enabled: &enabled,
						Params:  ProbeConfigParams{"host": "db1", "password": "resolved"},
					},
				},
			},
			configPath: configPath,
		}
	}

	target := newTarget()
	if target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "evil.example"}}) {
		t.Fatal("AdoptProbe retargeted a probe with a local secret, want false")
	}
	if target.cfg.Probes[0].Params["host"] != "db1" || target.cfg.Probes[0].Params["password"] != "resolved" {
		t.Fatalf("cfg changed on refused adopt: %#v", target.cfg.Probes[0].Params)
	}

	target = newTarget()
	if !target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "evil.example", "password": "${secret:fleet.db}"}}) {
		t.Fatal("AdoptProbe with its own secret = false, want true")
	}

	target = newTarget()
	if !target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"max_connections": 5}}) {
		t.Fatal("AdoptProbe without a target param = false, want true")
	}
}

func TestOverlayProbesListsSecretBearingParams(t *testing.T) {
	probeYAML := `- name: db
  type: postgresql
  params:
    host: db1
    password: plain-s3cret
    dsn: postgres://u:pw-in-url@h/db
    token: ${env:ADOPT_TEST_TOKEN}
    conn: ${env:ADOPT_TEST_CONN}
    url: https://u:mixed-pw@h/${env:ADOPT_TEST_TOKEN}
    api_key: ${secret:db.key}
    nested:
      community: inner-s3cret
      list: [plain-in-list]
`
	configPath := writeSecurityTestConfig(t, probeYAML)
	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{Name: "db", Type: "postgresql", Params: ProbeConfigParams{"host": "db1"}},
			},
		},
		configPath: configPath,
	}
	probes := target.Probes()
	if len(probes) != 1 || probes[0].Name != "db" {
		t.Fatalf("expected one reported probe named db, got %#v", probes)
	}
	got := make(map[string]bool, len(probes[0].SecretParams))
	for _, name := range probes[0].SecretParams {
		got[name] = true
	}
	for _, want := range []string{"password", "token", "conn", "api_key", "dsn", "url"} {
		if !got[want] {
			t.Errorf("SecretParams missing %q: %#v", want, probes[0].SecretParams)
		}
	}
	if got["host"] {
		t.Errorf("SecretParams contains host: %#v", probes[0].SecretParams)
	}
}

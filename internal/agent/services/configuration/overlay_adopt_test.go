package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/extension"
)

func adoptTestConfig() *LocalConfigurationData {
	enabled := true
	return &LocalConfigurationData{
		Probes: []ProbeConfig{
			{
				Name:    "db",
				Type:    "postgresql",
				Enabled: &enabled,
				Params: ProbeConfigParams{
					"host":     "db1",
					"password": "resolved-s3cret",
					"interval": 30,
				},
			},
		},
	}
}

func TestOverlayAdoptKeepsLocalParams(t *testing.T) {
	cfg := adoptTestConfig()
	target := &overlayTarget{cfg: cfg}
	ok := target.AdoptProbe(extension.ManagedProbe{
		Name:     "db",
		Type:     "postgresql",
		Enabled:  true,
		Interval: 120,
		Params:   map[string]any{"host": "db2"},
	})
	if !ok {
		t.Fatal("AdoptProbe refused a valid adoption")
	}
	if len(cfg.Probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(cfg.Probes))
	}
	probe := cfg.Probes[0]
	if probe.Params["host"] != "db2" {
		t.Errorf("host = %v, want db2", probe.Params["host"])
	}
	if probe.Params["password"] != "resolved-s3cret" {
		t.Errorf("password = %v, want resolved-s3cret", probe.Params["password"])
	}
	if probe.Params["interval"] != 120 {
		t.Errorf("interval = %v, want 120", probe.Params["interval"])
	}
	if probe.Enabled == nil || !*probe.Enabled {
		t.Error("Enabled = false, want true")
	}
}

func TestOverlayAdoptRefusals(t *testing.T) {
	disabled := false
	cases := []struct {
		name   string
		mutate func(*LocalConfigurationData)
		probe  extension.ManagedProbe
	}{
		{
			name:   "missing name",
			mutate: func(*LocalConfigurationData) {},
			probe:  extension.ManagedProbe{Name: "missing", Type: "postgresql", Enabled: true},
		},
		{
			name:   "type mismatch",
			mutate: func(*LocalConfigurationData) {},
			probe:  extension.ManagedProbe{Name: "db", Type: "mysql", Enabled: true},
		},
		{
			name: "local disabled",
			mutate: func(cfg *LocalConfigurationData) {
				cfg.Probes[0].Enabled = &disabled
			},
			probe: extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true},
		},
		{
			name:   "interval too small",
			mutate: func(*LocalConfigurationData) {},
			probe:  extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Interval: 5},
		},
		{
			name:   "params carry interval",
			mutate: func(*LocalConfigurationData) {},
			probe: extension.ManagedProbe{
				Name:    "db",
				Type:    "postgresql",
				Enabled: true,
				Params:  map[string]any{"interval": 60},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := adoptTestConfig()
			tc.mutate(cfg)
			before := reflect.ValueOf(cfg.Probes[0]).Interface()
			target := &overlayTarget{cfg: cfg}
			if target.AdoptProbe(tc.probe) {
				t.Fatal("AdoptProbe accepted a refusal case")
			}
			if !reflect.DeepEqual(cfg.Probes[0], before) {
				t.Errorf("probe changed: got %#v, want %#v", cfg.Probes[0], before)
			}
		})
	}
}

func TestOverlayAdoptRollback(t *testing.T) {
	cfg := adoptTestConfig()
	before := reflect.ValueOf(cfg.Probes[0]).Interface()
	target := &overlayTarget{cfg: cfg}
	if !target.AdoptProbe(extension.ManagedProbe{
		Name:    "db",
		Type:    "postgresql",
		Enabled: true,
		Params:  map[string]any{"host": "db2"},
	}) {
		t.Fatal("AdoptProbe refused a valid adoption")
	}
	target.rollback()
	if !reflect.DeepEqual(cfg.Probes[0], before) {
		t.Errorf("rollback did not restore probe: got %#v, want %#v", cfg.Probes[0], before)
	}
}

func TestOverlayReportNeverCarriesASecretInClear(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	agentYAML := "config_version: 3\nagent:\n  name: t\n"
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
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agentYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-db.yaml"), []byte(probeYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADOPT_TEST_TOKEN", "env-s3cret")
	t.Setenv("ADOPT_TEST_CONN", "postgres://x:env-dsn-pw@h/db")

	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{
					Name:   "db",
					Type:   "postgresql",
					Params: ProbeConfigParams{"password": "plain-s3cret"},
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
	for _, secret := range []string{"plain-s3cret", "pw-in-url", "env-s3cret", "env-dsn-pw", "inner-s3cret", "mixed-pw"} {
		if strings.Contains(s, secret) {
			t.Errorf("reported params contain secret %q: %s", secret, s)
		}
	}
	for _, want := range []string{"${env:ADOPT_TEST_TOKEN}", "${env:ADOPT_TEST_CONN}", "${secret:db.key}", "db1"} {
		if !strings.Contains(s, want) {
			t.Errorf("reported params missing %q: %s", want, s)
		}
	}
	if probes[0].EnvDefined {
		t.Error("EnvDefined = true, want false")
	}
}

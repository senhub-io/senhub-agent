package configuration

import (
	"errors"
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/services/extension"
)

func TestOverlayAdoptRetargetComparesWithLocalValue(t *testing.T) {
	prevResolve := resolveSecretFunc
	t.Cleanup(func() { resolveSecretFunc = prevResolve })
	resolveSecretFunc = func(name, _ string, _ bool) (string, error) {
		if name == "fleet.db" {
			return "fleet-db-secret", nil
		}
		return "", errors.New("unknown secret")
	}
	probeYAML := `- name: db
  type: postgresql
  params:
    host: db1
    password: ${secret:db.pw}
`
	configPath := writeSecurityTestConfig(t, probeYAML)
	newTarget := func(params ProbeConfigParams) *overlayTarget {
		enabled := true
		return &overlayTarget{
			cfg: &LocalConfigurationData{
				Probes: []ProbeConfig{
					{
						Name:    "db",
						Type:    "postgresql",
						Enabled: &enabled,
						Params:  params,
					},
				},
			},
			configPath: configPath,
		}
	}

	// Case 1: identical target params, no password -> adopt
	target := newTarget(ProbeConfigParams{"host": "db1", "password": "resolved"})
	if !target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "db1"}}) {
		t.Fatal("AdoptProbe with identical host and no password = false, want true")
	}

	// Case 2: changed host without password -> refuse, cfg unchanged
	target = newTarget(ProbeConfigParams{"host": "db1", "password": "resolved"})
	if target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "evil.example"}}) {
		t.Fatal("AdoptProbe retargeted a probe with a local secret, want false")
	}
	if target.cfg.Probes[0].Params["host"] != "db1" || target.cfg.Probes[0].Params["password"] != "resolved" {
		t.Fatalf("cfg changed on refused adopt: %#v", target.cfg.Probes[0].Params)
	}

	// Case 3: target param missing locally, no password -> refuse
	target = newTarget(ProbeConfigParams{"host": "db1", "password": "resolved"})
	if target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"endpoint": "x"}}) {
		t.Fatal("AdoptProbe with an unknown target param and no password = true, want false")
	}
	if target.cfg.Probes[0].Params["host"] != "db1" || target.cfg.Probes[0].Params["password"] != "resolved" {
		t.Fatalf("cfg changed on refused adopt: %#v", target.cfg.Probes[0].Params)
	}

	// Case 4: changed host with explicit password -> adopt
	target = newTarget(ProbeConfigParams{"host": "db1", "password": "resolved"})
	if !target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "evil.example", "password": "${secret:fleet.db}"}}) {
		t.Fatal("AdoptProbe with its own secret = false, want true")
	}

	// Case 5: host identical (string) and port 5432.0 vs local int 5432 -> adopt
	probeYAMLWithPort := `- name: db
  type: postgresql
  params:
    host: db1
    port: 5432
    password: ${secret:db.pw}
`
	configPathWithPort := writeSecurityTestConfig(t, probeYAMLWithPort)
	newTargetPort := func(params ProbeConfigParams) *overlayTarget {
		enabled := true
		return &overlayTarget{
			cfg: &LocalConfigurationData{
				Probes: []ProbeConfig{
					{
						Name:    "db",
						Type:    "postgresql",
						Enabled: &enabled,
						Params:  params,
					},
				},
			},
			configPath: configPathWithPort,
		}
	}
	target = newTargetPort(ProbeConfigParams{"host": "db1", "port": 5432, "password": "resolved"})
	if !target.AdoptProbe(extension.ManagedProbe{Name: "db", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "db1", "port": 5432.0}}) {
		t.Fatal("AdoptProbe with same target values (int vs float) = false, want true")
	}
}

func TestOverlayProbesTargetParams(t *testing.T) {
	probeYAML := `- name: db
  type: postgresql
  params:
    host: db1
    port: 5432
`
	configPath := writeSecurityTestConfig(t, probeYAML)
	enabled := true
	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{
				{
					Name:    "db",
					Type:    "postgresql",
					Enabled: &enabled,
					Params:  ProbeConfigParams{"host": "db1", "port": 5432},
				},
			},
		},
		configPath: configPath,
	}
	probes := target.Probes()
	if len(probes) != 1 {
		t.Fatalf("Probes() returned %d probes, want 1", len(probes))
	}
	want := []string{"host", "port"}
	if !reflect.DeepEqual(probes[0].TargetParams, want) {
		t.Fatalf("TargetParams = %v, want %v", probes[0].TargetParams, want)
	}
}

func TestOverlayAdoptRefusesRetargetOfUnreportedProbe(t *testing.T) {
	// The file declares only "db"; "mem" exists in memory alone, so its
	// secret-bearing list is unknown and a retarget cannot be proven safe.
	configPath := writeSecurityTestConfig(t, "- name: db\n  type: postgresql\n  params:\n    host: db1\n")
	enabled := true
	target := &overlayTarget{
		cfg: &LocalConfigurationData{
			Probes: []ProbeConfig{{Name: "mem", Type: "postgresql", Enabled: &enabled, Params: ProbeConfigParams{"host": "db1", "password": "resolved"}}},
		},
		configPath: configPath,
	}
	if target.AdoptProbe(extension.ManagedProbe{Name: "mem", Type: "postgresql", Enabled: true, Params: map[string]any{"host": "evil.example"}}) {
		t.Fatal("AdoptProbe retargeted a probe absent from the file report, want false")
	}
}

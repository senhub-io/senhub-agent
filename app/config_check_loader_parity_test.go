package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

const parityKey = "0f4c2a51-9b7e-4d3a-8c61-2e5f7a9b0d13"

func parityConfig(storage, probes string) string {
	return fmt.Sprintf("config_version: %d\nagent:\n  key: %s\n%s%s",
		configuration.CurrentConfigVersion, parityKey, storage, probes)
}

const (
	parityStorage = "storage:\n  - name: http\n    params:\n      port: 18080\n"
	parityProbes  = "probes:\n  - name: cpu\n    type: cpu\n"
)

// A validator that passes what the loader refuses is wrong: Ansible and
// the MSI apply a configuration on the strength of `config check`. For
// each condition the loader refuses, the check must report an error (exit
// 2); for each one the loader accepts, it must not.
func TestConfigCheckSeverityMatchesLoader(t *testing.T) {
	cases := []struct {
		name          string
		yaml          string
		loaderRefuses bool
	}{
		{"valid", parityConfig(parityStorage, parityProbes), false},
		{"no storage strategy", parityConfig("", parityProbes), true},
		{"empty storage list", parityConfig("storage: []\n", parityProbes), true},
		{"storage strategy without a name", parityConfig("storage:\n  - params:\n      port: 1\n", parityProbes), true},
		{"unknown storage strategy", parityConfig("storage:\n  - name: nonesuch\n  - name: http\n", parityProbes), false},
		{"no probes", parityConfig(parityStorage, ""), false},
		{"probe without a name", parityConfig(parityStorage, "probes:\n  - type: cpu\n"), true},
		{"probe without a type", parityConfig(parityStorage, "probes:\n  - name: cpu\n"), true},
		{"missing agent key", strings.Replace(parityConfig(parityStorage, parityProbes), "  key: "+parityKey+"\n", "", 1), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent-config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("writing configuration: %v", err)
			}

			zlog := zerolog.Nop()
			log := agentLogger.NewModuleLogger((*agentLogger.Logger)(&zlog), "test")
			data, err := configuration.LoadFromDisk(path, log)
			if err != nil {
				t.Fatalf("LoadFromDisk: %v", err)
			}
			loaderErr := configuration.ValidateConfiguration(&data)
			if (loaderErr != nil) != tc.loaderRefuses {
				t.Fatalf("loader refuses = %v (%v), table says %v", loaderErr != nil, loaderErr, tc.loaderRefuses)
			}

			var out checkOutcome
			captureStdout(t, func() { out = checkConfig(path) })
			if tc.loaderRefuses && out.exitCode() != 2 {
				t.Errorf("the loader refuses this configuration (%v) but config check exits %d", loaderErr, out.exitCode())
			}
			if !tc.loaderRefuses && out.errors > 0 {
				t.Errorf("the loader accepts this configuration but config check reports %d error(s)", out.errors)
			}
		})
	}
}

func TestConfigCheckNoStorageUsesTheLoaderWording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-config.yaml")
	if err := os.WriteFile(path, []byte(parityConfig("", parityProbes)), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { checkConfig(path) })
	if !strings.Contains(out, "[ERROR] at least one storage strategy is required") {
		t.Errorf("output does not carry the loader's wording as an error:\n%s", out)
	}
}

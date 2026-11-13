//go:build !windows

package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAdminKeyPresent(t *testing.T) {
	const agentYAML = "config_version: 3\nagent:\n  name: test\n"

	tests := []struct {
		name     string
		files    map[string]string
		expected bool
	}{
		{
			name: "literal admin key in strategies fragment",
			files: map[string]string{
				"agent.yaml":                agentYAML,
				"strategies.d/01-http.yaml": "http:\n  port: 8080\n  admin_key: \"k\"\n",
			},
			expected: true,
		},
		{
			name: "secret reference admin key in strategies fragment",
			files: map[string]string{
				"agent.yaml":                agentYAML,
				"strategies.d/01-http.yaml": "http:\n  port: 8080\n  admin_key: \"${secret:http.admin_key}\"\n",
			},
			expected: true,
		},
		{
			name: "http fragment without admin key",
			files: map[string]string{
				"agent.yaml":                agentYAML,
				"strategies.d/01-http.yaml": "http:\n  port: 8080\n",
			},
			expected: false,
		},
		{
			name: "no strategies dir and no storage block",
			files: map[string]string{
				"agent.yaml": agentYAML,
			},
			expected: true,
		},
		{
			// A monolithic file keeps its outputs in a sequence the key is
			// never added to: the layout split owns that case and warns.
			name: "legacy monolithic storage sequence",
			files: map[string]string{
				"agent.yaml": "config_version: 2\nagent:\n  key: \"k\"\nstorage:\n  - type: http\n    params: {port: 8080}\n",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeConfigFile(t, filepath.Join(dir, rel), content)
			}
			if got := adminKeyPresent(filepath.Join(dir, "agent.yaml")); got != tt.expected {
				t.Fatalf("adminKeyPresent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestReadOnlyConfigDirWarnsOnlyWhenAdminKeyMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission modes")
	}

	tests := []struct {
		name        string
		httpYAML    string
		wantWarning int
	}{
		{
			name:        "key present",
			httpYAML:    "http:\n  port: 8080\n  admin_key: \"k\"\n",
			wantWarning: 0,
		},
		{
			name:        "key missing",
			httpYAML:    "http:\n  port: 8080\n",
			wantWarning: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetWarnOnceForTest()

			dir := t.TempDir()
			agentPath := filepath.Join(dir, "agent.yaml")
			httpPath := filepath.Join(dir, "strategies.d", "01-http.yaml")
			writeConfigFile(t, agentPath, "config_version: 3\nagent:\n  name: test\n")
			writeConfigFile(t, httpPath, tt.httpYAML)

			before, err := os.ReadFile(httpPath)
			if err != nil {
				t.Fatalf("read http fragment before chmod: %v", err)
			}

			if err := os.Chmod(dir, 0500); err != nil {
				t.Fatalf("chmod dir read-only: %v", err)
			}
			t.Cleanup(func() {
				_ = os.Chmod(dir, 0700)
			})

			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			log := logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.adminkey.test")

			ensureAdminKeyOrWarn(agentPath, log)
			ensureAdminKeyOrWarn(agentPath, log)

			if got := strings.Count(buf.String(), "no administration key can be added"); got != tt.wantWarning {
				t.Fatalf("warning occurrences = %d, want %d; log: %s", got, tt.wantWarning, buf.String())
			}

			after, err := os.ReadFile(httpPath)
			if err != nil {
				t.Fatalf("read http fragment after calls: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("http fragment changed: before %q after %q", before, after)
			}
		})
	}
}

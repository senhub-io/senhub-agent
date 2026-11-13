package extensionhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestHostEffectiveConfigNeverCarriesASecret(t *testing.T) {
	dir := t.TempDir()

	probesDir := filepath.Join(dir, "probes.d")
	strategiesDir := filepath.Join(dir, "strategies.d")
	if err := os.MkdirAll(probesDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strategiesDir, 0o750); err != nil {
		t.Fatal(err)
	}

	agentYAML := "config_version: 3\nagent:\n  name: eff-test\n"
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(agentYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	probesYAML := `- name: mongo
  type: mongodb
  params:
    uri: mongodb://admin:mongo-literal-pw@db1:27017
- name: mongoenv
  type: mongodb
  params:
    uri: ${env:EFF_TEST_MONGO_URI}
- name: ora
  type: oracle_enterprise
  params:
    dsn: oracle://scott:oracle-dsn-pw@orahost:1521/ORCL
- name: pg
  type: postgresql
  params:
    host: pg1
    password: pg-literal-pw
`
	if err := os.WriteFile(filepath.Join(probesDir, "10-db.yaml"), []byte(probesYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	strategiesYAML := `otlp:
  endpoint: collector:4317
  headers:
    Cookie: session=cookie-s3cret
    Authorization: Bearer bearer-s3cret
    X-Scope-OrgID: tenant-a
`
	if err := os.WriteFile(filepath.Join(strategiesDir, "10-otlp.yaml"), []byte(strategiesYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("EFF_TEST_MONGO_URI", "mongodb://u:env-uri-pw@db2:27017")

	h := &host{deps: Deps{Logger: nopLogger(), Config: &fakeConfig{path: path}},
		log: logger.NewModuleLogger(nopLogger(), "extension")}

	files, err := h.EffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	for _, f := range files {
		sb.WriteString(f.Name)
		sb.WriteString("\n")
		sb.WriteString(string(f.Body))
		sb.WriteString("\n")
	}
	text := sb.String()

	secrets := []string{
		"mongo-literal-pw",
		"env-uri-pw",
		"oracle-dsn-pw",
		"pg-literal-pw",
		"cookie-s3cret",
		"bearer-s3cret",
	}
	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			offending := ""
			for _, f := range files {
				if strings.Contains(string(f.Body), secret) {
					offending = f.Name
					break
				}
			}
			t.Errorf("EffectiveConfig leaked secret %q in file %q", secret, offending)
		}
	}

	for _, want := range []string{"tenant-a", "pg1", "collector:4317"} {
		if !strings.Contains(text, want) {
			t.Errorf("EffectiveConfig missing expected non-secret value %q", want)
		}
	}
}

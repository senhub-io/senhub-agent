package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactionMasksDsnUriAndCookie(t *testing.T) {
	dir := t.TempDir()

	agentYAML := "config_version: 3\nagent:\n  name: redact-test\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agentYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}

	mongoYAML := `- name: mongo
  type: mongodb
  params:
    uri: mongodb://admin:mongo-literal-pw@db1:27017
- name: mongoenv
  type: mongodb
  params:
    uri: ${env:REDACT_TEST_MONGO_URI}
- name: ora
  type: oracle_enterprise
  params:
    dsn: oracle://scott:oracle-dsn-pw@orahost:1521/ORCL
    host_label: orahost
`
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-mongo.yaml"), []byte(mongoYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	otlpYAML := `otlp:
  endpoint: collector:4317
  headers:
    Cookie: session=cookie-s3cret
    X-Scope-OrgID: tenant-a
`
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "10-otlp.yaml"), []byte(otlpYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("REDACT_TEST_MONGO_URI", "mongodb://u:env-uri-pw@db2:27017")

	secrets := []string{"mongo-literal-pw", "env-uri-pw", "oracle-dsn-pw", "cookie-s3cret"}
	readable := []string{"orahost", "tenant-a", "collector:4317"}

	data, err := LoadForShow(filepath.Join(dir, "agent.yaml"), ShowRedact, nil)
	if err != nil {
		t.Fatal(err)
	}
	marshaled, err := MarshalSortedYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(marshaled)
	for _, s := range secrets {
		if strings.Contains(text, s) {
			t.Errorf("LoadForShow(ShowRedact) output contains secret %q:\n%s", s, text)
		}
	}
	for _, r := range readable {
		if !strings.Contains(text, r) {
			t.Errorf("LoadForShow(ShowRedact) output missing readable value %q:\n%s", r, text)
		}
	}

	files, err := RedactedConfigFiles(filepath.Join(dir, "agent.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var combined strings.Builder
	for _, f := range files {
		combined.WriteString(f.Name)
		combined.WriteString("\n")
		combined.Write(f.Body)
		combined.WriteString("\n")
	}
	filesText := combined.String()
	for _, s := range secrets {
		if strings.Contains(filesText, s) {
			t.Errorf("RedactedConfigFiles output contains secret %q in:\n%s", s, filesText)
		}
	}
	for _, r := range []string{"tenant-a", "collector:4317"} {
		if !strings.Contains(filesText, r) {
			t.Errorf("RedactedConfigFiles output missing readable value %q in:\n%s", r, filesText)
		}
	}
}

package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactionMasksURLUserinfoUnderAnyKey(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("agent.yaml", "config_version: 3\nagent:\n  name: redact-test\n")
	write("probes.d/10-web.yaml", `- name: web
  type: http_check
  params:
    targets:
      - http://lab:URLpw-list@127.0.0.1:9/
    note: see http://lab:URLpw-plain@host.example/x
    nested:
      endpoint: https://svc:URLpw-nested@host.example:8443/api
`)
	write("strategies.d/10-http.yaml", `http:
  endpoint: https://svc:URLpw-endpoint@collector.example/ingest
`)

	leaks := []string{"URLpw-list", "URLpw-plain", "URLpw-nested", "URLpw-endpoint", "lab:", "svc:"}
	cfg := filepath.Join(dir, "agent.yaml")

	data, err := LoadForShow(cfg, ShowRedact, nil)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := MarshalSortedYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := RedactedConfigFiles(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sent strings.Builder
	for _, f := range files {
		sent.Write(f.Body)
	}

	for name, text := range map[string]string{"config show --redact": string(shown), "effective configuration": sent.String()} {
		for _, leak := range leaks {
			if strings.Contains(text, leak) {
				t.Errorf("%s shows %q:\n%s", name, leak, text)
			}
		}
		if !strings.Contains(text, "http://***@127.0.0.1:9/") {
			t.Errorf("%s: want scheme://***@host form for the target:\n%s", name, text)
		}
	}
}

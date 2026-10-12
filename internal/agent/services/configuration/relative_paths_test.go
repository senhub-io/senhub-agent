package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

// Relative file paths mean the configuration directory, whatever the
// working directory of the process that loads the file (#606).
func TestLoadFromDisk_RelativePathsFollowTheConfigurationDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(cfg, []byte("config_version: 3\nagent:\n  key: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	frag := `- name: pg
  type: postgresql
  params:
    host: db.internal
    metrics_path: relative/url/path
    tls:
      ca_file: certs/ca.pem
    bookmark_path: state/pg.bookmark
    cert_file: /etc/ssl/abs.pem
    key_file: ${env:NOT_SET_ANYWHERE_606}
`
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-pg.yaml"), []byte(frag), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	data, err := LoadFromDisk(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := data.Probes[0].Params
	tlsBlock, _ := p["tls"].(map[string]interface{})
	absDir, _ := filepath.Abs(dir)
	if got, want := tlsBlock["ca_file"], filepath.Join(absDir, "certs/ca.pem"); got != want {
		t.Errorf("tls.ca_file = %v, want %v", got, want)
	}
	if got, want := p["bookmark_path"], filepath.Join(absDir, "state/pg.bookmark"); got != want {
		t.Errorf("bookmark_path = %v, want %v", got, want)
	}
	if got := p["cert_file"]; got != "/etc/ssl/abs.pem" {
		t.Errorf("an absolute path must stay as written, got %v", got)
	}
	if got := p["metrics_path"]; got != "relative/url/path" {
		t.Errorf("a *_path key that is not a file must be left alone, got %v", got)
	}
}

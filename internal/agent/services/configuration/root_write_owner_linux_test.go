//go:build linux

package configuration

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// `sudo senhub-agent config set` and `sudo senhub-agent secret set` run as
// root against a configuration the service account owns. Both used to leave
// a root-owned 0600 file behind, which the service could not read: the
// reload failed and the next start failed with it.
func TestRootWritesKeepTheServiceOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: ownership after a privileged write is under test")
	}
	const uid, gid = 4242, 4343
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 3\nagent:\n  key: \"k\"\n")
	writeSealFile(t, filepath.Join(dir, "strategies.d", "00-http.yaml"), "http:\n  port: 8080\n")
	for _, p := range []string{dir, cfg, filepath.Join(dir, "strategies.d"), filepath.Join(dir, "strategies.d", "00-http.yaml")} {
		if err := os.Chown(p, uid, gid); err != nil {
			t.Fatal(err)
		}
	}

	if err := SetStrategyScalar(cfg, "http", "bind_address", "0.0.0.0", ""); err != nil {
		t.Fatalf("SetStrategyScalar: %v", err)
	}
	p, err := secret.NewAgeKeyfileProvider(filepath.Join(dir, "agent-secret.key"), filepath.Join(dir, "secrets.age"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Set("otlp.token", secret.New("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := WriteLicenseSidecar(cfg, "eyJ.recette.sig"); err != nil {
		t.Fatalf("WriteLicenseSidecar: %v", err)
	}

	for _, f := range []string{"strategies.d/00-http.yaml", "agent-secret.key", "secrets.age", "license.jwt"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		st := fi.Sys().(*syscall.Stat_t)
		if st.Uid != uid || st.Gid != gid {
			t.Errorf("%s is owned by %d:%d after a root write, want the service's %d:%d", f, st.Uid, st.Gid, uid, gid)
		}
	}
}

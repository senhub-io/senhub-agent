package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrator_KeepsCommentsAndAConcurrentOperatorEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeSealFile(t, path, "# operator note\nprobes:\n  - name: cpu # the cpu\n    params:\n      interval: 30\n")

	setCommitHook(t, func(p string, attempt int) {
		if attempt != 1 {
			return
		}
		edited := "# operator note\nprobes:\n  - name: cpu # the cpu\n    params:\n      interval: 99 # raised by operator\n"
		if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	m := NewConfigMigrator(path, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err := m.MigrateIfNeeded(); err != nil {
		t.Fatalf("MigrateIfNeeded: %v", err)
	}
	got := read(t, path)
	for _, want := range []string{"# operator note", "# the cpu", "interval: 99", "# raised by operator", "type: cpu", "config_version: 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("migrated file lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "interval: 30") {
		t.Errorf("the operator's edit was overwritten:\n%s", got)
	}
}

func TestMigrator_GivesUpWithoutWritingWhenTheFileKeepsChanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	original := "probes:\n  - name: cpu\n"
	writeSealFile(t, path, original)
	i := 0
	setCommitHook(t, func(p string, _ int) {
		i++
		if err := os.WriteFile(p, []byte(original+strings.Repeat("#", i)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	m := NewConfigMigrator(path, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err := m.MigrateIfNeeded(); !errors.Is(err, ErrConfigChangedConcurrently) {
		t.Fatalf("err = %v, want ErrConfigChangedConcurrently", err)
	}
	if strings.Contains(read(t, path), "type:") {
		t.Error("the file was migrated despite the give-up")
	}
}

func TestEnsureAdminKey_KeepsAConcurrentOperatorEdit(t *testing.T) {
	cfg := writeInstall(t, "http:\n  port: 8080\n  endpoints: [\"web\"]\n")
	fragment := filepath.Join(filepath.Dir(cfg), "strategies.d", "00-http.yaml")
	setCommitHook(t, func(p string, attempt int) {
		if attempt == 1 && p == fragment {
			if err := os.WriteFile(p, []byte("http:\n  port: 9999 # operator\n  endpoints: [\"web\"]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err := EnsureAdminKey(cfg, nil); err != nil {
		t.Fatalf("EnsureAdminKey: %v", err)
	}
	got := httpFragment(t, cfg)
	if !strings.Contains(got, "port: 9999") || !strings.Contains(got, "# operator") || !strings.Contains(got, "admin_key:") {
		t.Errorf("the operator's edit or the key is missing:\n%s", got)
	}
}

func TestEnsureAdminKey_GivesUpWithoutWritingWhenTheFileKeepsChanging(t *testing.T) {
	cfg := writeInstall(t, "http:\n  port: 8080\n")
	fragment := filepath.Join(filepath.Dir(cfg), "strategies.d", "00-http.yaml")
	i := 0
	setCommitHook(t, func(p string, _ int) {
		if p != fragment {
			return
		}
		i++
		if err := os.WriteFile(p, []byte("http:\n  port: 8080\n"+strings.Repeat("#", i)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := EnsureAdminKey(cfg, nil); !errors.Is(err, ErrConfigChangedConcurrently) {
		t.Fatalf("err = %v, want ErrConfigChangedConcurrently", err)
	}
	if strings.Contains(httpFragment(t, cfg), "admin_key") {
		t.Error("a key was written despite the give-up")
	}
}

func TestMigrateLicenseToSidecar_RollbackKeepsAConcurrentOperatorEdit(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\nagent:\n  key: k\n  license: eyJ-inline-jwt\nlog:\n  level: info\n")

	commits := 0
	setCommitHook(t, func(p string, attempt int) {
		if p != cfg || attempt != 1 {
			return
		}
		commits++
		switch commits {
		case 1:
			// While the inline field is being cleared, the sidecar is
			// replaced: the verification then sees another license and
			// the migration must be undone.
			if err := os.WriteFile(LicenseSidecarPath(cfg), []byte("tampered\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		case 2:
			// While the undo puts the inline field back, an operator saves.
			if err := os.WriteFile(p, []byte("config_version: 2\nagent:\n  key: k\nlog:\n  level: debug # operator\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	})

	if err := MigrateLicenseToSidecar(cfg, nil); err == nil {
		t.Fatal("expected the verification to fail")
	}
	got := read(t, cfg)
	if !strings.Contains(got, "level: debug") || !strings.Contains(got, "# operator") {
		t.Errorf("the rollback overwrote the operator's edit:\n%s", got)
	}
	if !strings.Contains(got, "license: eyJ-inline-jwt") {
		t.Errorf("the inline license was not put back:\n%s", got)
	}
	if _, err := os.Stat(LicenseSidecarPath(cfg)); !os.IsNotExist(err) {
		t.Errorf("the sidecar must be removed by the rollback, err=%v", err)
	}
}

func TestSealRollback_RestoresOnlyTheSealsChangeAndKeepsAConcurrentEdit(t *testing.T) {
	secret.SetProvider(&mismatchProvider{inner: secret.NewMemoryProvider()})
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\n")
	probe := filepath.Join(dir, "probes.d", "10-db.yaml")
	writeSealFile(t, probe, "- type: mysql\n  params:\n    password: real-secret\n")

	setCommitHook(t, func(p string, attempt int) {
		// Only the rollback's commit sees the file already sealed.
		b := read(t, p)
		if p != probe || attempt != 1 || !strings.Contains(b, "${secret:") {
			return
		}
		edited := strings.Replace(b, "params:\n", "params:\n    timeout: 7 # operator\n", 1)
		if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	err := SealInlineSecrets(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "backups restored") {
		t.Fatalf("expected a verify failure with a clean restore, got %v", err)
	}
	got := read(t, probe)
	if !strings.Contains(got, "real-secret") || strings.Contains(got, "${secret:") {
		t.Errorf("plaintext not restored:\n%s", got)
	}
	if !strings.Contains(got, "timeout: 7") || !strings.Contains(got, "# operator") {
		t.Errorf("the operator's edit was overwritten by the pre-edit backup:\n%s", got)
	}
}

func TestSealRollback_AFileUntouchedSinceTheSealGoesBackByteForByte(t *testing.T) {
	secret.SetProvider(&mismatchProvider{inner: secret.NewMemoryProvider()})
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.yaml")
	writeSealFile(t, cfg, "config_version: 2\n")
	probe := filepath.Join(dir, "probes.d", "10-db.yaml")
	original := "- type: mysql\n  params:\n    password:   real-secret   # odd spacing\n"
	writeSealFile(t, probe, original)

	if err := SealInlineSecrets(cfg, nil); err == nil {
		t.Fatal("expected a verify failure")
	}
	if got := read(t, probe); got != original {
		t.Errorf("an untouched file must come back byte for byte:\n%q\nwant\n%q", got, original)
	}
}

package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

func setCommitHook(t *testing.T, h func(path string, attempt int)) {
	t.Helper()
	beforeCommitHook.Store(&h)
	t.Cleanup(func() { beforeCommitHook.Store(nil) })
}

func TestSealAgentKey_KeepsAnOperatorEditLandingBetweenReadAndWrite(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeSealFile(t, path, "# operator note\nagent:\n  key: plain-key-123\nlog:\n  level: info\n")

	setCommitHook(t, func(p string, attempt int) {
		if attempt != 1 {
			return
		}
		edited := "# operator note\nagent:\n  key: plain-key-123\nlog:\n  level: debug # raised by operator\n"
		if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	n, _, err := sealAgentKeyInFile(path, newSealState())
	if err != nil || n != 1 {
		t.Fatalf("sealAgentKeyInFile = %d, %v", n, err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	for _, want := range []string{"level: debug", "# raised by operator", "# operator note", "${secret:agent.key}"} {
		if !strings.Contains(s, want) {
			t.Errorf("result lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "plain-key-123") {
		t.Errorf("plaintext key still in file:\n%s", s)
	}
	if v, _ := mp.Get("agent.key"); v != "plain-key-123" {
		t.Errorf("stored key = %q", v)
	}
}

func TestRewriteFile_GivesUpWithoutWritingWhenTheFileKeepsChanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeSealFile(t, path, "a: 0\n")

	calls := 0
	setCommitHook(t, func(p string, attempt int) {
		if err := os.WriteFile(p, []byte("a: "+strings.Repeat("x", attempt)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	wrote, err := rewriteFile(path, func(data []byte) ([]byte, error) {
		calls++
		return []byte("edited: true\n"), nil
	})
	if wrote || !errors.Is(err, ErrConfigChangedConcurrently) {
		t.Fatalf("wrote=%v err=%v, want give-up", wrote, err)
	}
	if calls != maxRewriteAttempts {
		t.Errorf("edit ran %d times, want %d", calls, maxRewriteAttempts)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "edited") {
		t.Errorf("file was written despite give-up: %q", got)
	}
}

func TestSealAgentKey_GiveUpLeavesTheFileUntouchedAndSealRetriesNextStart(t *testing.T) {
	mp := secret.NewMemoryProvider()
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeSealFile(t, path, "agent:\n  key: plain-key-123\n")

	i := 0
	setCommitHook(t, func(p string, _ int) {
		i++
		body := "agent:\n  key: plain-key-123\n" + strings.Repeat("#", i) + "\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if _, _, err := sealAgentKeyInFile(path, newSealState()); !errors.Is(err, ErrConfigChangedConcurrently) {
		t.Fatalf("err = %v, want ErrConfigChangedConcurrently", err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "${secret:") {
		t.Errorf("file sealed despite give-up:\n%s", got)
	}

	beforeCommitHook.Store(nil)
	if n, _, err := sealAgentKeyInFile(path, newSealState()); err != nil || n != 1 {
		t.Fatalf("retry = %d, %v", n, err)
	}
}

func TestSetRootConfigVersion_KeepsAConcurrentEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeSealFile(t, path, "# keep me\nlog:\n  level: info\n")
	setCommitHook(t, func(p string, attempt int) {
		if attempt == 1 {
			if err := os.WriteFile(p, []byte("# keep me\nlog:\n  level: warn\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err := setRootConfigVersion(path, 3); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"level: warn", "# keep me", "config_version: 3"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("result lacks %q:\n%s", want, got)
		}
	}
}

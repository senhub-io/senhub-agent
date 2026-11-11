//go:build !windows

package configuration

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestReadOnlyConfigDirSkipsWrites(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission modes")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("config_version: 3\nagent:\n  name: test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	log := logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.readonly.test")

	EnsureDefaultProbeFragments(configPath, log, DefaultFragmentOptions{})
	EnsureDefaultProbeFragments(configPath, log, DefaultFragmentOptions{})

	assertOnlyConfigFiles(t, dir)

	if n := strings.Count(buf.String(), "not writable"); n != 1 {
		t.Fatalf("expected exactly one not-writable warning over two calls, got %d\n%s", n, buf.String())
	}

	if err := SealInlineSecrets(configPath, log); !errors.Is(err, secret.ErrConfigDirReadOnly) {
		t.Fatalf("SealInlineSecrets error = %v, want ErrConfigDirReadOnly", err)
	}
	assertOnlyConfigFiles(t, dir)
}

func assertOnlyConfigFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "agent.yaml" && e.Name() != "probes.d" {
			t.Fatalf("unexpected file created in read-only config dir: %s", e.Name())
		}
	}
}

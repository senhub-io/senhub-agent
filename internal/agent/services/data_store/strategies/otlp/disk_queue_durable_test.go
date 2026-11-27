package otlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileDurablyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "batch.dat")

	data := []byte("otlp payload")
	if err := writeFileDurably(path, data, 0o640); err != nil {
		t.Fatalf("writeFileDurably: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content = %q, want %q", got, data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestWriteFileDurablyMissingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "batch.dat")

	if err := writeFileDurably(path, []byte("data"), 0o640); err == nil {
		t.Fatal("expected error for missing directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target file should not exist, stat err = %v", err)
	}
}

func TestWriteFileDurablyReplacesShorterContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "batch.dat")

	if err := os.WriteFile(path, []byte("much longer original content"), 0o640); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := writeFileDurably(path, []byte("short"), 0o640); err != nil {
		t.Fatalf("writeFileDurably: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "short" {
		t.Fatalf("content = %q, want %q", got, "short")
	}
}

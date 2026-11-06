package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeRootConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindUnknownRootKeysKnownOnly(t *testing.T) {
	path := writeRootConfig(t, `
config_version: 2
agent:
  key: abc
probes: []
storage: []
logs: {}
entities: {}
governance: {}
auto_update: {}
cache: {}
`)
	unknown, err := FindUnknownRootKeys(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown keys, got %v", unknown)
	}
}

func TestFindUnknownRootKeysTypos(t *testing.T) {
	path := writeRootConfig(t, `
agent:
  key: abc
agnet:
  key: abc
entitie: {}
`)
	unknown, err := FindUnknownRootKeys(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"agnet", "entitie"}
	if !reflect.DeepEqual(unknown, want) {
		t.Fatalf("expected %v, got %v", want, unknown)
	}
}

func TestFindUnknownRootKeysCommentOnly(t *testing.T) {
	path := writeRootConfig(t, "# just a comment\n")
	unknown, err := FindUnknownRootKeys(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unknown != nil {
		t.Fatalf("expected nil, got %v", unknown)
	}
}

func TestFindUnknownRootKeysInvalidYAML(t *testing.T) {
	path := writeRootConfig(t, "agent: [unclosed\n")
	if _, err := FindUnknownRootKeys(path); err == nil {
		t.Fatal("expected an error for invalid YAML")
	}
}

func TestKnownRootKeys(t *testing.T) {
	keys := KnownRootKeys()
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	for _, want := range []string{"agent", "probes"} {
		if !has[want] {
			t.Errorf("KnownRootKeys missing %q", want)
		}
	}
	if has["extensions"] {
		t.Error(`KnownRootKeys must not contain the inline field name "extensions"`)
	}
	if has[""] {
		t.Error("KnownRootKeys must not contain an empty key")
	}
}

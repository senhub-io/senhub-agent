package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeAgentKeysConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestFindUnknownAgentKeys(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"valid block", "agent:\n  key: k\n  license: l\n  global_tags:\n    site: paris\n", nil},
		{"update_check_interval", "agent:\n  update_check_interval: 60\n", []string{"update_check_interval"}},
		{"typo and unknown", "agent:\n  licence: abc\n  mode: fast\n", []string{"licence", "mode"}},
		{"no agent block", "probes: []\n", nil},
		{"agent scalar", "agent: x\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindUnknownAgentKeys(writeAgentKeysConfig(t, tc.content))
			if err != nil {
				t.Fatalf("FindUnknownAgentKeys: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("FindUnknownAgentKeys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFindUnknownAgentKeys_Unreadable(t *testing.T) {
	if _, err := FindUnknownAgentKeys(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

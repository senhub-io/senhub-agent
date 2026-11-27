package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMonolithicSplitBlocker(t *testing.T) {
	tests := []struct {
		name      string
		multiFile bool
		setup     func(t *testing.T, dir string)
		wantErr   bool
	}{
		{
			name: "monolithic file alone blocks nothing",
			setup: func(t *testing.T, dir string) {
				t.Helper()
			},
		},
		{
			name: "install defaults beside monolithic file block nothing",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				seedInstallDefaults(t, dir)
			},
		},
		{
			name: "operator fragment beside monolithic file blocks the split",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeFragment(t, filepath.Join(dir, "probes.d", "10-mine.yaml"), "- name: mine\n  type: cpu\n")
			},
			wantErr: true,
		},
		{
			name:      "multi-file layout never blocks",
			multiFile: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeFragment(t, filepath.Join(dir, "probes.d", "10-mine.yaml"), "- name: mine\n  type: cpu\n")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			content := monolithicHTTPConfig
			if tc.multiFile {
				content = "agent:\n  licence: test\n"
			}
			configPath := filepath.Join(dir, "agent.yaml")
			if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			err := MonolithicSplitBlocker(configPath)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("MonolithicSplitBlocker: unexpected error: %v", err)
				}
				return
			}
			var fbErr *FragmentsBesideMonolithicError
			if !errors.As(err, &fbErr) {
				t.Fatalf("MonolithicSplitBlocker: want *FragmentsBesideMonolithicError, got %T (%v)", err, err)
			}
			want := filepath.Join(dir, "probes.d", "10-mine.yaml")
			found := false
			for _, p := range fbErr.Fragments {
				if p == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("FragmentsBesideMonolithicError.Fragments: want %s in %v", want, fbErr.Fragments)
			}
		})
	}
}

func writeFragment(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

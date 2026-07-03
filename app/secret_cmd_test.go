package app

import (
	"os"
	"path/filepath"
	"testing"
)

// secretHasFlag decides whether `secret rm` skips its confirmation
// prompt. The arguments passed in are os.Args[2:], i.e. they include
// the sub-verb ("rm") and the positional name.
func TestSecretHasFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{"present after name", []string{"rm", "grafana", "--yes"}, "--yes", true},
		{"present before name", []string{"rm", "--yes", "grafana"}, "--yes", true},
		{"absent", []string{"rm", "grafana"}, "--yes", false},
		{"empty args", nil, "--yes", false},
		{"different flag only", []string{"rm", "grafana", "--config-path", "/x"}, "--yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretHasFlag(tc.args, tc.flag); got != tc.want {
				t.Errorf("secretHasFlag(%v, %q) = %v, want %v", tc.args, tc.flag, got, tc.want)
			}
		})
	}
}

// secretArgName must never mistake the VALUE of a value-taking flag
// (--config-path, --from-file) for the <name> positional — the M2 bug that
// stored/read/deleted secrets under a path instead of their real name.
func TestSecretArgName(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{"plain name", []string{"get", "veeam.password"}, "veeam.password", false},
		{"config-path before name", []string{"get", "--config-path", "/etc/senhub", "veeam.password"}, "veeam.password", false},
		{"from-file before name", []string{"set", "--from-file", "/tmp/pw", "veeam.password"}, "veeam.password", false},
		{"name before flags", []string{"rm", "veeam.password", "--config-path", "/etc/senhub", "--yes"}, "veeam.password", false},
		{"boolean flag skipped", []string{"rm", "--yes", "veeam.password"}, "veeam.password", false},
		{"missing name is error", []string{"get", "--config-path", "/etc/senhub"}, "", true},
		{"missing name bare verb", []string{"list"}, "", true},
		{"two positionals is error", []string{"set", "a", "b"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := secretArgName(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("secretArgName(%v) = %q, nil; want error", tc.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("secretArgName(%v) unexpected error: %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("secretArgName(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// readSecretValue reads --from-file without the value ever crossing argv,
// and trims a trailing newline so an editor-saved file round-trips cleanly.
func TestReadSecretValue_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pw")
	if err := os.WriteFile(path, []byte("s3cr3t\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readSecretValue([]string{"set", "db.password", "--from-file", path})
	if err != nil {
		t.Fatalf("readSecretValue: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("readSecretValue = %q, want %q (CRLF trimmed)", got, "s3cr3t")
	}
}

func TestReadSecretValue_FromFileMissingPath(t *testing.T) {
	if _, err := readSecretValue([]string{"set", "db.password", "--from-file"}); err == nil {
		t.Error("--from-file with no path should error")
	}
}

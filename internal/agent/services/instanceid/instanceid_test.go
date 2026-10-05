package instanceid

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	randomKey = "9b2f6c1e-3d4a-4e5b-8c7d-0a1b2c3d4e5f" // UUID v4
	legacyKey = "my-short-agent-key"
	idA       = "11111111-2222-5333-8444-555555555555"
	idB       = "66666666-7777-5888-8999-aaaaaaaaaaaa"
)

func TestIsRandomKey(t *testing.T) {
	cases := map[string]bool{
		randomKey:                              true,
		"9B2F6C1E-3D4A-4E5B-8C7D-0A1B2C3D4E5F": true,
		legacyKey:                              false,
		"":                                     false,
		"pending":                              false,
		"9b2f6c1e-3d4a-1e5b-8c7d-0a1b2c3d4e5f": false, // version 1, not random
		"9b2f6c1e-3d4a-4e5b-cc7d-0a1b2c3d4e5f": false, // not the RFC 4122 variant
		"9b2f6c1e3d4a4e5b8c7d0a1b2c3d4e5f":     false,
	}
	for key, want := range cases {
		if got := IsRandomKey(key); got != want {
			t.Errorf("IsRandomKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestWrite_ModeFollowsTheKeyEntropy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	cases := []struct {
		name string
		key  string
		want os.FileMode
	}{
		{"random uuid key is world readable", randomKey, 0o644},
		{"legacy key is private", legacyKey, 0o600},
		{"empty key is private", "", 0o600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := Write(dir, idA, tc.key); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(filepath.Join(dir, FileName))
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != tc.want {
				t.Errorf("mode = %o, want %o", got, tc.want)
			}
			got, err := Read(dir)
			if err != nil || got != idA {
				t.Errorf("Read = %q, %v; want %q", got, err, idA)
			}
		})
	}
}

// A file left world readable by an earlier run with a random key must be
// tightened when the key turns out to be a guessable one.
func TestWrite_ReplacesTheModeOfAnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(idB+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, idA, legacyKey); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
	if got, _ := Read(dir); got != idA {
		t.Errorf("Read = %q, want %q", got, idA)
	}
}

func TestRead_RejectsWhatIsNotAnInstanceID(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil {
		t.Error("missing file must be an error")
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("not an id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Error("garbage must be an error")
	}
}

func TestIsAgentProcess(t *testing.T) {
	cases := []struct {
		name, exe string
		want      bool
	}{
		{"senhub-agent", "", true},
		{"senhub-agent.exe", "", true},
		{"SenHub-Agent.EXE", "", true},
		{"", "/usr/local/bin/senhub-agent", true},
		{"", `C:\Program Files\SenHub\senhub-agent.exe`, true},
		{"nginx", "/usr/sbin/nginx", false},
		{"senhub-agent-helper", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := IsAgentProcess(tc.name, tc.exe); got != tc.want {
			t.Errorf("IsAgentProcess(%q, %q) = %v, want %v", tc.name, tc.exe, got, tc.want)
		}
	}
}

func TestStateDirsFor(t *testing.T) {
	def := DefaultStateDir()
	got := StateDirsFor([]string{"/usr/bin/senhub-agent", "run", "--config-path", "/opt/b/agent.yaml"})
	if len(got) != 2 || got[0] != filepath.Dir("/opt/b/agent.yaml") || got[1] != def {
		t.Errorf("dirs = %v, want [/opt/b %s]", got, def)
	}
	got = StateDirsFor([]string{"senhub-agent", "run", "--config-path=/srv/c/agent.yaml"})
	if len(got) != 2 || got[0] != filepath.Dir("/srv/c/agent.yaml") {
		t.Errorf("dirs = %v", got)
	}
	if got = StateDirsFor(nil); len(got) != 1 || got[0] != def {
		t.Errorf("dirs = %v, want only the default", got)
	}
}

// Two agents on one host: each publishes its own id in its own state
// directory, and each reads the other's, never its own.
func TestTwoAgentsSeeEachOther(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := Write(dirA, idA, randomKey); err != nil {
		t.Fatal(err)
	}
	if err := Write(dirB, idB, randomKey); err != nil {
		t.Fatal(err)
	}
	cmdA := []string{"senhub-agent", "run", "--config-path", filepath.Join(dirA, "agent.yaml")}
	cmdB := []string{"senhub-agent", "run", "--config-path", filepath.Join(dirB, "agent.yaml")}

	if got, err := ReadForProcess(cmdB, idA); err != nil || got != idB {
		t.Errorf("A reading B = %q, %v; want %q", got, err, idB)
	}
	if got, err := ReadForProcess(cmdA, idB); err != nil || got != idA {
		t.Errorf("B reading A = %q, %v; want %q", got, err, idA)
	}
}

func TestReadForProcess_NeverReturnsTheCallersOwnID(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, idA, randomKey); err != nil {
		t.Fatal(err)
	}
	cmd := []string{"senhub-agent", "run", "--config-path", filepath.Join(dir, "agent.yaml")}
	if got, err := ReadForProcess(cmd, idA); err == nil {
		t.Errorf("an agent must not resolve another process to its own id, got %q", got)
	}
}

func TestOwnStateDir_HonoursSystemd(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", "/var/lib/senhub-agent-b:/var/lib/other")
	if got := OwnStateDir(); got != "/var/lib/senhub-agent-b" {
		t.Errorf("OwnStateDir = %q", got)
	}
	t.Setenv("STATE_DIRECTORY", "")
	if got := OwnStateDir(); got != DefaultStateDir() {
		t.Errorf("OwnStateDir = %q, want default", got)
	}
}

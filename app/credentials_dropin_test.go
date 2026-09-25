package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSyncDropInWritesRewritesAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "senhub-agent.service.d", "10-senhub-credentials.conf")
	body := "[Service]\nLoadCredentialEncrypted=agent.key:/etc/senhub-agent/creds.d/agent.key.cred\n"

	steps := []struct {
		name        string
		body        string
		wantChanged bool
		wantFile    bool
	}{
		{"nothing sealed, nothing there", "", false, false},
		{"first seal writes it", body, true, true},
		{"same store leaves it alone", body, false, true},
		{"another secret rewrites it", body + "LoadCredentialEncrypted=otlp.token:/x\n", true, true},
		{"emptied store removes it", "", true, false},
	}
	for _, s := range steps {
		changed, err := syncDropIn(path, s.body)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if changed != s.wantChanged {
			t.Errorf("%s: changed = %v, want %v", s.name, changed, s.wantChanged)
		}
		got, readErr := os.ReadFile(path)
		if s.wantFile && (readErr != nil || string(got) != s.body) {
			t.Errorf("%s: file = %q (%v), want %q", s.name, got, readErr, s.body)
		}
		if !s.wantFile && !os.IsNotExist(readErr) {
			t.Errorf("%s: drop-in still present", s.name)
		}
	}
}

func TestUnitConfigPath(t *testing.T) {
	cases := map[string]string{
		`ExecStart=/usr/local/bin/senhub-agent "run" "--config-path" "/etc/senhub-agent/agent.yaml"`:       "/etc/senhub-agent/agent.yaml",
		`ExecStart=/opt/senhub\x20agent/senhub-agent "run" "--config-path" "/opt/senhub agent/agent.yaml"`: "/opt/senhub agent/agent.yaml",
		`ExecStart=/usr/local/bin/senhub-agent run --config-path=/srv/agent.yaml`:                          "/srv/agent.yaml",
		`ExecStart=/usr/local/bin/senhub-agent run`:                                                        "",
	}
	for line, want := range cases {
		if got := unitConfigPath(line); got != want {
			t.Errorf("unitConfigPath(%s) = %q, want %q", line, got, want)
		}
	}
}

func TestUnitArgsHonoursEscapedQuotes(t *testing.T) {
	got := unitArgs(`"run" "--tag" "a \"b\" c"`)
	want := []string{"run", "--tag", `a "b" c`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unitArgs = %q, want %q", got, want)
	}
}

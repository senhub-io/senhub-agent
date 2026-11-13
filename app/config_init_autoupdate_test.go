package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/cliexit"
)

func TestParseInitConfigArgs_AutoUpdate(t *testing.T) {
	on, off := true, false
	cases := []struct {
		arg  string
		want *bool
		bad  bool
	}{
		{"1", &on, false},
		{"true", &on, false},
		{" TRUE ", &on, false},
		{"0", &off, false},
		{"false", &off, false},
		{"", nil, false},
		{"yes", nil, true},
	}
	for _, c := range cases {
		opts, err := parseInitConfigArgs([]string{"--auto-update", c.arg})
		if c.bad {
			if err == nil {
				t.Errorf("--auto-update %q: want an error", c.arg)
			}
			continue
		}
		if err != nil {
			t.Errorf("--auto-update %q: %v", c.arg, err)
			continue
		}
		switch {
		case c.want == nil && opts.autoUpdate != nil:
			t.Errorf("--auto-update %q: want the default, got %v", c.arg, *opts.autoUpdate)
		case c.want != nil && (opts.autoUpdate == nil || *opts.autoUpdate != *c.want):
			t.Errorf("--auto-update %q: want %v, got %v", c.arg, *c.want, opts.autoUpdate)
		}
	}
	if opts, err := parseInitConfigArgs(nil); err != nil || opts.autoUpdate != nil {
		t.Errorf("without the flag: want the default, got %+v, %v", opts, err)
	}
}

// The MSI passes --auto-update 1 on a fresh install; a kept configuration
// keeps its own setting, and the run says the flag was not applied. The
// real generator writes the file, since the setting lives in its template.
func TestConfigInitWritesTheAutoUpdateAsked(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	run := func(want int, argv ...string) string {
		t.Helper()
		var out bytes.Buffer
		env := initEnv{generate: defaultInitEnv().generate, checkPort: func(string, int) error { return nil }}
		if code := runConfigInit(argv, &out, env); code != want {
			t.Fatalf("config init %v: exit %d, want %d\n%s", argv, code, want, out.String())
		}
		return out.String()
	}

	on := filepath.Join(t.TempDir(), "agent.yaml")
	run(cliexit.OK, "--config-path", on, "--auto-update", "1")
	if !strings.Contains(read(on), "auto_update:\n  enabled: true") {
		t.Errorf("--auto-update 1 must write enabled: true:\n%s", read(on))
	}

	def := filepath.Join(t.TempDir(), "agent.yaml")
	run(cliexit.OK, "--config-path", def)
	if !strings.Contains(read(def), "auto_update:\n  enabled: false") {
		t.Errorf("without the flag the default stays disabled:\n%s", read(def))
	}

	out := run(cliexit.Unchanged, "--config-path", on, "--auto-update", "0")
	if !strings.Contains(out, "--auto-update was ignored") {
		t.Errorf("a kept configuration must say the flag was ignored:\n%s", out)
	}
	if !strings.Contains(read(on), "auto_update:\n  enabled: true") {
		t.Errorf("a kept configuration keeps its setting:\n%s", read(on))
	}
}

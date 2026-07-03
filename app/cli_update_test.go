package app

import "testing"

func TestParseUpdateArg(t *testing.T) {
	cases := []struct {
		name        string
		arg         string
		wantVersion string
		wantHelp    bool
	}{
		{"long help", "--help", "", true},
		{"short help", "-h", "", true},
		{"bare help", "help", "", true},
		{"long list", "--list", "list", false},
		{"short list", "-l", "list", false},
		{"explicit version", "0.4.1", "0.4.1", false},
		{"latest keyword", "latest", "latest", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotHelp := parseUpdateArg(tc.arg)
			if gotVersion != tc.wantVersion {
				t.Errorf("parseUpdateArg(%q) version = %q, want %q", tc.arg, gotVersion, tc.wantVersion)
			}
			if gotHelp != tc.wantHelp {
				t.Errorf("parseUpdateArg(%q) wantHelp = %v, want %v", tc.arg, gotHelp, tc.wantHelp)
			}
		})
	}
}

// Regression: `update --help` used to be treated as a version literal,
// so the dispatch tried to install a release named "--help". It must
// resolve to the help path, never to a version.
func TestParseUpdateArg_HelpIsNotAVersion(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "help"} {
		version, wantHelp := parseUpdateArg(arg)
		if !wantHelp {
			t.Errorf("parseUpdateArg(%q) should request help", arg)
		}
		if version != "" {
			t.Errorf("parseUpdateArg(%q) leaked version %q; help must carry no target", arg, version)
		}
	}
}

// `update --help` is informational and must bypass the privilege gate,
// like `--help` and `config check`; a real `update <version>` must not.
func TestReadOnlyCommand_UpdateHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"update --help", []string{"agent", "update", "--help"}, true},
		{"update -h", []string{"agent", "update", "-h"}, true},
		{"update help", []string{"agent", "update", "help"}, true},
		{"update version", []string{"agent", "update", "0.4.1"}, false},
		{"update list", []string{"agent", "update", "--list"}, false},
		{"bare update", []string{"agent", "update"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := readOnlyCommand(tc.args); got != tc.want {
				t.Errorf("readOnlyCommand(%v) = %v, want %v", tc.args[1:], got, tc.want)
			}
		})
	}
}

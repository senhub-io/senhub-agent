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

// `update --help` and `update --list` are informational (usage text or a
// registry read, no binary write) and must bypass the privilege gate, like
// `--help` and `config check`; a real `update <version>` — and a bare
// `update`, which installs the newest release — must not.
func TestReadOnlyCommand_UpdateHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"update --help", []string{"agent", "update", "--help"}, true},
		{"update -h", []string{"agent", "update", "-h"}, true},
		{"update help", []string{"agent", "update", "help"}, true},
		{"update --list", []string{"agent", "update", "--list"}, true},
		{"update -l", []string{"agent", "update", "-l"}, true},
		{"update version", []string{"agent", "update", "0.4.1"}, false},
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

// parseUpdateCommand must honour the documented flags and reject malformed
// invocations rather than treating a stray flag as a version literal.
func TestParseUpdateCommand(t *testing.T) {
	t.Run("bare update checks for newest", func(t *testing.T) {
		got, wantHelp, err := parseUpdateCommand(nil)
		if err != nil || wantHelp {
			t.Fatalf("parseUpdateCommand(nil) = help=%v err=%v, want no help/err", wantHelp, err)
		}
		if got.WantedVersion != "" {
			t.Errorf("WantedVersion = %q, want empty (check-newest)", got.WantedVersion)
		}
	})

	t.Run("help forms", func(t *testing.T) {
		for _, a := range []string{"--help", "-h", "help"} {
			_, wantHelp, err := parseUpdateCommand([]string{a})
			if err != nil || !wantHelp {
				t.Errorf("parseUpdateCommand(%q) = help=%v err=%v, want help", a, wantHelp, err)
			}
		}
	})

	t.Run("list mode", func(t *testing.T) {
		got, _, err := parseUpdateCommand([]string{"--list"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.WantedVersion != "list" {
			t.Errorf("WantedVersion = %q, want \"list\"", got.WantedVersion)
		}
	})

	t.Run("list rejects extra args", func(t *testing.T) {
		if _, _, err := parseUpdateCommand([]string{"--list", "0.4.1"}); err == nil {
			t.Error("update --list 0.4.1 should be rejected")
		}
	})

	t.Run("version honours dry-run", func(t *testing.T) {
		got, _, err := parseUpdateCommand([]string{"0.4.1", "--dry-run"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.WantedVersion != "0.4.1" {
			t.Errorf("WantedVersion = %q, want 0.4.1", got.WantedVersion)
		}
		if !got.DryRun {
			t.Error("DryRun = false, want true (--dry-run must be honoured)")
		}
	})

	t.Run("version honours registry-url", func(t *testing.T) {
		got, _, err := parseUpdateCommand([]string{"0.4.1", "--registry-url", "http://test"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.UpdateRegistryUrl != "http://test" {
			t.Errorf("UpdateRegistryUrl = %q, want http://test", got.UpdateRegistryUrl)
		}
	})

	t.Run("unknown flag rejected not taken as version", func(t *testing.T) {
		if _, _, err := parseUpdateCommand([]string{"--bogus"}); err == nil {
			t.Error("update --bogus should be rejected, not treated as a version")
		}
	})
}

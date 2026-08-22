package configuration

import "testing"

// TestNormalizeRegistryURL covers every shape someone has plausibly
// written into auto_update.url. The base must come out able to take
// BOTH suffixes the agent appends — /releases/... for the version list
// and /download/... for artifacts — because a value that only works for
// one of them fails silently on the other.
func TestNormalizeRegistryURL(t *testing.T) {
	const base = "https://eu-west-1.intake.senhub.io"

	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{"already a base", base, base, false},
		{"trailing slash only", base + "/", base, true},

		// The shape the pre-#586 installer scaffolded, and the one an
		// operator writes after seeing a /releases link.
		{"releases suffix", base + "/releases", base, true},
		{"releases with slash", base + "/releases/", base, true},

		// Someone who pasted the version-list URL itself.
		{"version list url", base + "/releases/releases.json", base, true},
		{"beta list url", base + "/releases/beta/releases.json", base, true},
		{"beta directory", base + "/releases/beta", base, true},

		// Someone who pasted a download link's directory. This one the
		// old single-suffix version did not handle at all.
		{"download suffix", base + "/download", base, true},

		// A shape a previous half-fix could have left behind.
		{"doubled releases", base + "/releases/releases", base, true},

		// A mirror genuinely served from a subdirectory must survive:
		// the agent cannot tell an intended path from a mistake, and
		// breaking a working configuration to fix a broken one is worse.
		{"operator subdirectory", "https://mirror.example/artifacts", "https://mirror.example/artifacts", false},
		{"operator subdirectory with port", "http://10.0.0.1:8080/senhub", "http://10.0.0.1:8080/senhub", false},

		{"empty", "", "", false},
		{"whitespace", "   ", "   ", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := NormalizeRegistryURL(c.in)
			if got != c.want {
				t.Errorf("NormalizeRegistryURL(%q) = %q, want %q", c.in, got, c.want)
			}
			if changed != c.changed {
				t.Errorf("NormalizeRegistryURL(%q) changed = %v, want %v", c.in, changed, c.changed)
			}
		})
	}
}

// TestNormalizeRegistryURLIsIdempotent guards the property the whole fix
// rests on: whatever came out is already a base, so normalising twice
// changes nothing. Without it, a value could oscillate between two forms
// depending on which code path read it.
func TestNormalizeRegistryURLIsIdempotent(t *testing.T) {
	inputs := []string{
		"https://eu-west-1.intake.senhub.io/releases",
		"https://eu-west-1.intake.senhub.io/releases/releases.json",
		"https://eu-west-1.intake.senhub.io/download",
		"https://mirror.example/artifacts",
	}
	for _, in := range inputs {
		once, _ := NormalizeRegistryURL(in)
		twice, changed := NormalizeRegistryURL(once)
		if changed || twice != once {
			t.Errorf("not idempotent for %q: %q then %q (changed=%v)", in, once, twice, changed)
		}
	}
}

// TestNormalizeRegistryURLKeepsUnusableValues pins the boundary between
// this function and the validator. A value with nothing left after
// stripping cannot be repaired into a base, so it is returned untouched
// and CheckRegistryURL is what tells the operator.
func TestNormalizeRegistryURLKeepsUnusableValues(t *testing.T) {
	for _, in := range []string{"/releases", "releases.json", "not a url"} {
		got, changed := NormalizeRegistryURL(in)
		if changed {
			t.Errorf("NormalizeRegistryURL(%q) = %q, changed — an unusable value must be left for the validator", in, got)
		}
	}
}

func TestCheckRegistryURL(t *testing.T) {
	const base = "https://eu-west-1.intake.senhub.io"

	if p := CheckRegistryURL(base); p != nil {
		t.Errorf("a correct base was reported as a problem: %+v", p)
	}
	if p := CheckRegistryURL(""); p != nil {
		t.Errorf("an absent value means the built-in default and must not be a problem: %+v", p)
	}

	p := CheckRegistryURL(base + "/releases")
	if p == nil {
		t.Fatal("a doubled path was not reported")
	}
	if p.Suggestion != base {
		t.Errorf("suggestion = %q, want %q — the operator needs the value to write, not just the diagnosis", p.Suggestion, base)
	}

	// Unusable values get an error with no suggestion, because there is
	// nothing to suggest.
	for _, bad := range []string{"not a url", "ftp://example.com", "/releases"} {
		p := CheckRegistryURL(bad)
		if p == nil {
			t.Errorf("CheckRegistryURL(%q) reported no problem", bad)
			continue
		}
		if p.Suggestion != "" {
			t.Errorf("CheckRegistryURL(%q) suggested %q for a value it cannot repair", bad, p.Suggestion)
		}
	}
}

// TestRegistryURLWarningIsOncePerValue pins the reason the warning moved
// off the read path: GetConfiguration resolves the auto-update block and
// runs once per datapoint batch, so an unconditional warning there means
// one identical line per batch for the life of the process.
func TestRegistryURLWarningIsOncePerValue(t *testing.T) {
	ResetRegistryURLWarningsForTest()

	const v = "https://example.com/releases"
	if !ShouldWarnRegistryURL(v) {
		t.Fatal("first occurrence should warn")
	}
	for i := 0; i < 100; i++ {
		if ShouldWarnRegistryURL(v) {
			t.Fatalf("warned again on occurrence %d", i+2)
		}
	}
	// A different bad value still deserves its own warning.
	if !ShouldWarnRegistryURL("https://other.example/download") {
		t.Error("a different value should warn on its first occurrence")
	}
}

package configuration

import "testing"

// #747: hosts installed before #586 carry auto_update.url with a trailing
// /releases. The agent appends /releases/releases.json itself, so the request
// went to .../releases/releases/releases.json — a 404 — and auto-update did
// nothing while the config still said enabled: true.
func TestNormalizeRegistryURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{
			name:    "the field-observed broken value",
			in:      "https://eu-west-1.intake.senhub.io/releases",
			want:    "https://eu-west-1.intake.senhub.io",
			changed: true,
		},
		{
			name:    "same with a trailing slash",
			in:      "https://eu-west-1.intake.senhub.io/releases/",
			want:    "https://eu-west-1.intake.senhub.io",
			changed: true,
		},
		{
			name:    "a correct base URL is left alone",
			in:      "https://eu-west-1.intake.senhub.io",
			want:    "https://eu-west-1.intake.senhub.io",
			changed: false,
		},
		{
			name:    "a bare trailing slash is trimmed",
			in:      "https://eu-west-1.intake.senhub.io/",
			want:    "https://eu-west-1.intake.senhub.io",
			changed: true,
		},
		{
			name: "a path that merely CONTAINS releases is untouched — an " +
				"operator mirror may legitimately serve from such a path",
			in:      "https://mirror.example.com/senhub/releases/stable",
			want:    "https://mirror.example.com/senhub/releases/stable",
			changed: false,
		},
		{
			name:    "empty stays empty rather than becoming a bare scheme",
			in:      "",
			want:    "",
			changed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := NormalizeRegistryURL(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeRegistryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if changed != tc.changed {
				t.Errorf("NormalizeRegistryURL(%q) changed = %v, want %v",
					tc.in, changed, tc.changed)
			}
		})
	}
}

// The normalisation only pays off if it produces the URL the updater actually
// requests. This pins the two together, so a change to either side that breaks
// the pairing fails here rather than in the field, where the symptom is
// silence.
func TestNormalizedURLYieldsTheVersionListPath(t *testing.T) {
	const wantList = "https://eu-west-1.intake.senhub.io/releases/releases.json"

	for _, configured := range []string{
		"https://eu-west-1.intake.senhub.io",
		"https://eu-west-1.intake.senhub.io/",
		"https://eu-west-1.intake.senhub.io/releases",
		"https://eu-west-1.intake.senhub.io/releases/",
	} {
		base, _ := NormalizeRegistryURL(configured)
		got := base + "/releases/releases.json"
		if got != wantList {
			t.Errorf("configured %q resolves to %q, want %q", configured, got, wantList)
		}
	}
}

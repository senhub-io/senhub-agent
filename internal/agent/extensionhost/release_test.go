package extensionhost

import "testing"

func TestReleaseChannel(t *testing.T) {
	cases := []struct {
		includeBeta bool
		want        string
	}{
		{false, "stable"},
		{true, "beta"},
	}
	for _, c := range cases {
		if got := releaseChannel(c.includeBeta); got != c.want {
			t.Errorf("releaseChannel(%v) = %q, want %q", c.includeBeta, got, c.want)
		}
	}
}

func TestReleaseRing(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://registry.example.com/updates", "all"},
		{"https://registry.example.com/ring/pilots", "pilots"},
		{"https://registry.example.com/ring/pilots/", "pilots"},
		{"https://registry.example.com/updates/ring/canary/feed.xml", "canary"},
		{"https://registry.example.com/ring/", "all"},
		{"", "all"},
		{"://bad-url", "all"},
	}
	for _, c := range cases {
		if got := releaseRing(c.url); got != c.want {
			t.Errorf("releaseRing(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

package auto_update

import (
	"errors"
	"testing"
)

func TestShouldUpdateTo(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		expected string
		want     bool
		wantErr  bool
	}{
		// The motivating regression: prod-release "latest" cannot
		// downgrade a beta that is on a higher minor.
		{"beta refuses prod downgrade", "0.1.94-beta", "0.1.91", false, false},
		// Promotion from beta to its release: pre-release < release
		// per semver, so 0.1.94 > 0.1.94-beta — the agent updates.
		{"beta to release of same triplet upgrades", "0.1.94-beta", "0.1.94", true, false},
		// Same string is a no-op (handled earlier by caller but we
		// preserve safety here too).
		{"identical version stays put", "0.1.94", "0.1.94", false, false},
		// Old agent picking up a newer release.
		{"prod minor bump upgrades", "0.1.91", "0.1.92", true, false},
		// Beta of a higher minor still beats prod of a lower one.
		{"prod upgrades to next-minor beta", "0.1.91", "0.1.92-beta", true, false},
		// Downgrade across minors is refused.
		{"prod refuses downgrade across minors", "0.2.0", "0.1.99", false, false},
		// Numbered betas (X.Y.Z-beta.N) follow semver pre-release rules:
		// the identifiers compare one by one, numerically when numeric.
		{"next numbered beta upgrades", "0.6.2-beta.2", "0.6.2-beta.3", true, false},
		{"older numbered beta is refused", "0.6.2-beta.3", "0.6.2-beta.2", false, false},
		{"beta.10 outranks beta.9", "0.6.2-beta.9", "0.6.2-beta.10", true, false},
		{"numbered beta to its release upgrades", "0.6.2-beta.3", "0.6.2", true, false},
		{"release refuses its own numbered beta", "0.6.2", "0.6.2-beta.3", false, false},
		{"numbered beta to next-patch beta upgrades", "0.6.2-beta.3", "0.6.3-beta.1", true, false},
		{"same numbered beta stays put", "0.6.2-beta.3", "0.6.2-beta.3", false, false},
		{"legacy beta to numbered beta of the same triplet", "0.6.2-beta", "0.6.2-beta.1", true, false},
		{"dev build sorts before its release", "0.6.2-dev.57.g1a2b3c4d", "0.6.2", true, false},
		// Parse failure on either side is fail-closed.
		{"unparseable current fails closed", "latest-dev", "0.1.91", false, true},
		{"unparseable expected fails closed", "0.1.91", "??-not-a-version", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shouldUpdateTo(tc.current, tc.expected)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("shouldUpdateTo(%q, %q) = %v, want %v", tc.current, tc.expected, got, tc.want)
			}
		})
	}
}

func TestErrFirst_ReturnsFirstNonNil(t *testing.T) {
	if got := errFirst(nil, nil, nil); got != nil {
		t.Errorf("all-nil should return nil; got %v", got)
	}
	mock := &mockErr{"first"}
	if got := errFirst(nil, mock, nil); !errors.Is(got, mock) {
		t.Errorf("got %v, want first non-nil mock", got)
	}
	second := &mockErr{"second"}
	if got := errFirst(nil, mock, second); !errors.Is(got, mock) {
		t.Errorf("got %v, want %v (first non-nil)", got, mock)
	}
}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }

func TestGetLatestVersion_NumberedBetas(t *testing.T) {
	versions := []VersionMetadata{
		{Name: "0.6.2-beta.2", Version: "0.6.2-beta.2"},
		{Name: "0.6.2-beta.10", Version: "0.6.2-beta.10"},
		{Name: "0.6.2-beta.9", Version: "0.6.2-beta.9"},
		{Name: "0.6.1", Version: "0.6.1"},
	}
	if got := GetLatestVersion(versions); got == nil || got.Version != "0.6.2-beta.10" {
		t.Fatalf("GetLatestVersion() = %v, want 0.6.2-beta.10", got)
	}
	versions = append(versions, VersionMetadata{Name: "0.6.2", Version: "0.6.2"})
	if got := GetLatestVersion(versions); got == nil || got.Version != "0.6.2" {
		t.Fatalf("GetLatestVersion() = %v, want 0.6.2", got)
	}
}

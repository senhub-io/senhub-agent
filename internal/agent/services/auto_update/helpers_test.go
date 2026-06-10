package auto_update

import "testing"

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
	if got := errFirst(nil, mock, nil); got != mock {
		t.Errorf("got %v, want first non-nil mock", got)
	}
	second := &mockErr{"second"}
	if got := errFirst(nil, mock, second); got != mock {
		t.Errorf("got %v, want %v (first non-nil)", got, mock)
	}
}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }

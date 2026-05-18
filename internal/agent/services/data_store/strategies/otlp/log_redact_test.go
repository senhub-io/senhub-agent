package otlp

import (
	"strings"
	"testing"
)

func TestRedactSensitive_StripsBearerToken(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "grpc unauthenticated echo (hex token)",
			in:   "export: failed to upload metrics: rpc error: code = Unauthenticated desc = scheme or token does not match: Bearer 0577b821b8afbb43b78569ff7897adef20e0adb447efda9df498c909077d80a4",
			want: "Bearer ***",
		},
		{
			name: "uuid-format token",
			in:   "scheme or token does not match: Bearer 4947726a-3e85-4706-8be1-134d83e2a29f-03e2c318",
			want: "Bearer ***",
		},
		{
			name: "case-insensitive header echo",
			in:   "auth header: bearer abc123def",
			want: "Bearer ***",
		},
		{
			name: "no bearer present is identity",
			in:   "rpc error: code = DeadlineExceeded desc = context deadline exceeded",
			want: "rpc error: code = DeadlineExceeded desc = context deadline exceeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSensitive(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("redactSensitive(%q) = %q; missing %q", tc.in, got, tc.want)
			}
			// Negative: no surviving bearer + non-whitespace token sequence.
			if strings.Contains(got, "Bearer 0577") ||
				strings.Contains(got, "Bearer 4947") ||
				strings.Contains(got, "bearer abc") {
				t.Errorf("redaction leaked the original token: %q", got)
			}
		})
	}
}

func TestRedactSensitive_LeavesNonBearerWordsAlone(t *testing.T) {
	// The pattern must not eat words that simply contain "bearer" as a
	// substring or stand-alone (e.g. an error message describing the
	// auth scheme without echoing a value).
	in := "expected bearer-token-auth extension; check your collector config"
	got := redactSensitive(in)
	if got != in {
		t.Errorf("non-credential-bearing 'bearer' was rewritten: %q -> %q", in, got)
	}
}

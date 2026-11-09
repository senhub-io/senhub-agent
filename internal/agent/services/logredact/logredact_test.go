package logredact

import "testing"

func TestString(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "userinfo masked",
			input: "checking https://user:s3cretPW@h.example.com/x",
			want:  "checking https://***@h.example.com/x",
		},
		{
			name:  "query token masked",
			input: "https://h.example.com/x?token=tok3nVAL&ok=1",
			want:  "https://h.example.com/x?token=***&ok=***",
		},
		{
			name:  "query api_key masked",
			input: "https://h.example.com/x?api_key=tok3nVAL",
			want:  "https://h.example.com/x?api_key=***",
		},
		{
			name:  "query password masked",
			input: "https://h.example.com/x?password=s3cretPW",
			want:  "https://h.example.com/x?password=***",
		},
		{
			name:  "fragment masked",
			input: "https://h.example.com/x?token=tok3nVAL#fr4gSEC",
			want:  "https://h.example.com/x?token=***#***",
		},
		{
			name:  "authorization bearer masked",
			input: "Authorization: Bearer " + jwt,
			want:  "Authorization: Bearer ***",
		},
		{
			name:  "authorization basic masked",
			input: "Authorization: Basic czNjcjN0UFcxMjM0NTY=",
			want:  "Authorization: Basic ***",
		},
		{
			name:  "authorization digest masked",
			input: `Authorization: Digest username="u", realm="r", response="abc123def456\"`,
			want:  `Authorization: Digest ***`,
		},
		{
			name:  "jwt masked",
			input: "token is " + jwt + " ok",
			want:  "token is *** ok",
		},
		{
			name:  "cloud access key id masked",
			input: "key AKIAFAKEFAKEFAKE1234 leaked",
			want:  "key *** leaked",
		},
		{
			name:  "clean url unchanged",
			input: "https://h.example.com/x?ok=1",
			want:  "https://h.example.com/x?ok=***",
		},
		{
			name:  "plain text unchanged",
			input: "starting http_check probe on host h.example.com",
			want:  "starting http_check probe on host h.example.com",
		},
		{
			name:  "dsn password masked",
			input: "host=h password=s3cretPW dbname=db",
			want:  "host=h password=*** dbname=db",
		},
		{
			name:  "generic sensitive pair masked",
			input: "client_secret=s3cretPW author=me",
			want:  "client_secret=*** author=me",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := String(tc.input); got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestStringCleanURLFullyUnchanged(t *testing.T) {
	in := "https://h.example.com/path only text # not a url fragment"
	// '#' in plain text is left alone; the URL has no query so only the
	// fragment rule could apply, and it requires a scheme.
	if got := String(in); got != in {
		t.Errorf("String(%q) = %q, want unchanged", in, got)
	}
}

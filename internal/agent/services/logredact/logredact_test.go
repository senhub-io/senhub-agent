package logredact

import (
	"strings"
	"testing"
)

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

func TestRedactCountsEachMaskedFormOnce(t *testing.T) {
	in := `checking https://user:s3cretPW@h.example.com/x?x=plainVAL&api_key=tok3nVAL#fr4gSEC: Get "https://h.example.com/p?x=1: ok" key AKIAFAKEFAKEFAKE1234 eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln`
	counts := map[string]int{}
	out := Redact(in, func(rule string) { counts[rule]++ })
	for _, want := range []string{"?x=***&api_key=***#*** Get", "?x=*** ok", "https://***@h.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for rule, n := range map[string]int{RuleURLUserinfo: 1, RuleURLQuery: 2, RuleURLFragment: 1, RuleAWSAccessKeyID: 1, RuleJWT: 1} {
		if counts[rule] != n {
			t.Errorf("rule %s counted %d, want %d (%v)", rule, counts[rule], n, counts)
		}
	}
	again := map[string]int{}
	if out2 := Redact(out, func(rule string) { again[rule]++ }); out2 != out || len(again) != 0 {
		t.Errorf("second pass changed the text or counted: %q %v", out2, again)
	}
}

func TestRedactNeverKeepsTheLastCharacterOfASecret(t *testing.T) {
	for _, in := range []string{
		"https://h/x?password=hunter2!) next",
		"https://h/x#fr4g!, next",
		"https://h/x?k=abc: next",
		"https://h/x#abc: next",
	} {
		out := String(in)
		for _, leak := range []string{"hunter2", "fr4g", "abc"} {
			if strings.Contains(out, leak) {
				t.Errorf("%q leaks %q: %q", in, leak, out)
			}
		}
		if !strings.HasSuffix(out, " next") || !strings.Contains(out, "***") {
			t.Errorf("%q: unexpected output %q", in, out)
		}
	}
	if got := String("https://h/x# next"); got != "https://h/x# next" {
		t.Errorf("bare # changed: %q", got)
	}
}

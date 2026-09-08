package http

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func TestGuessField(t *testing.T) {
	spec.Register(spec.Probe{Type: "hint_probe", DisplayName: "x", Params: []spec.ParamSpec{
		{Key: "host", Kind: spec.KindString},
		{Key: "port", Kind: spec.KindInt},
		{Key: "username", Kind: spec.KindString},
		{Key: "password", Kind: spec.KindString, Secret: true},
		{Key: "tls", Kind: spec.KindBlock, Fields: []spec.ParamSpec{{Key: "ca_file", Kind: spec.KindString}}},
	}})
	cases := map[string]string{
		"Error 1045: Access denied for user 'monitor'@'10.0.0.1' (using password: YES)": "password",
		"dial tcp 10.0.0.5:3306: connect: connection refused":                           "host",
		"lookup db.example.internal: no such host":                                      "host",
		"tls.ca_file: open /etc/ca.pem: no such file or directory":                      "tls.ca_file",
		"x509: certificate signed by unknown authority":                                 "tls",
		"the report is empty": "",
		"":                    "",
	}
	for msg, want := range cases {
		if got := guessField("hint_probe", msg); got != want {
			t.Errorf("%q: want %q, got %q", msg, want, got)
		}
	}
	if guessField("no_such_probe", "password") != "" {
		t.Error("a probe without a schema has no hint")
	}
}

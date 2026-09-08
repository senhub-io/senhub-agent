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

func TestSplitParameterProblemsAndDeadTarget(t *testing.T) {
	if got := splitParameterProblems("parameters: port: must be a number; tls.ca_file: must be a string"); len(got) != 2 || got[0] != "port: must be a number" {
		t.Errorf("problems must be split one per entry, got %v", got)
	}
	if got := splitParameterProblems("the probe refuses this configuration: x"); len(got) != 1 {
		t.Errorf("another message stays whole, got %v", got)
	}
	metrics := []PreviewMetric{{Name: "senhub.db.up", Value: 0, Tags: map[string]string{"host": "127.0.0.1:1"}}, {Name: "mysql.uptime", Value: 12}}
	if msg := deadTarget(metrics); msg != "the target did not answer (127.0.0.1:1): senhub.db.up = 0" {
		t.Errorf("an availability metric at zero is a dead target, got %q", msg)
	}
	if msg := deadTarget([]PreviewMetric{{Name: "senhub.db.up", Value: 1}, {Name: "cpu.usage", Value: 0}}); msg != "" {
		t.Errorf("a live target passes, got %q", msg)
	}
}

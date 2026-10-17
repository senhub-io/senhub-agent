package app

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestConfigCheckReportsTheOpAMPBlock(t *testing.T) {
	const token = "alid_a_CHECKTOKEN"
	cases := []struct {
		name         string
		cfg          *configuration.OpAMPConfig
		wantErrors   int
		wantWarnings int
		want         string
	}{
		{"absent", nil, 0, 0, ""},
		{"disabled", &configuration.OpAMPConfig{Token: token}, 0, 0, "[OFF]  opamp: disabled"},
		{"enabled without endpoint nor token", &configuration.OpAMPConfig{Enabled: true}, 2, 0, "opamp.endpoint is required"},
		{"valid", &configuration.OpAMPConfig{Enabled: true, Endpoint: "wss://u:pw@opamp.example/v1/opamp?k=q", Token: token},
			0, 0, "[OK]   opamp: reporting to wss://u:xxxxx@opamp.example/v1/opamp over websocket every 30s"},
		{"insecure", &configuration.OpAMPConfig{Enabled: true, Endpoint: "ws://opamp.example/v1/opamp", Token: token,
			TLS: configuration.OpAMPTLSConfig{InsecureSkipVerify: true}}, 0, 1, "insecure_skip_verify"},
		{"plaintext to a remote host", &configuration.OpAMPConfig{Enabled: true, Endpoint: "ws://opamp.example/v1/opamp", Token: token},
			1, 0, "sends the token in clear"},
	}
	for _, c := range cases {
		var errs, warns int
		out := captureStdout(t, func() { errs, warns = reportOpAMP(c.cfg) })
		if errs != c.wantErrors || warns != c.wantWarnings {
			t.Errorf("%s: errors=%d warnings=%d, want %d/%d\n%s", c.name, errs, warns, c.wantErrors, c.wantWarnings, out)
		}
		if c.want == "" && out != "" {
			t.Errorf("%s: printed although the block is absent:\n%s", c.name, out)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: output lacks %q:\n%s", c.name, c.want, out)
		}
		if strings.Contains(out, token) || strings.Contains(out, "pw@") {
			t.Errorf("%s: a credential reached the check output:\n%s", c.name, out)
		}
	}
}

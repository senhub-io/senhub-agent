package configuration

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

func TestEnableHTTPSKeepsBindAddress(t *testing.T) {
	cases := []struct {
		name string
		bind string
		want string
	}{
		{"default stays loopback", "", `"127.0.0.1"`},
		{"explicit bind is kept", "10.1.2.3", `"10.1.2.3"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := &cliArgs.ParsedArgs{
				ConfigPath:      t.TempDir() + "/agent.yaml",
				EnableHttps:     true,
				HttpsPort:       8443,
				HttpBindAddress: tc.bind,
			}
			lc := NewLocalConfiguration(args, createTestLocalLogger())
			out := lc.generateHTTPStrategyFragment("key")
			if strings.Contains(out, "0.0.0.0") {
				t.Errorf("fragment widens the bind address:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("fragment lacks bind_address %s:\n%s", tc.want, out)
			}
			if !strings.Contains(out, "8443") {
				t.Errorf("fragment lacks the HTTPS port:\n%s", out)
			}
		})
	}
}

func TestEnableHTTPSLegacyDefaultKeepsLoopback(t *testing.T) {
	args := &cliArgs.ParsedArgs{EnableHttps: true, HttpsPort: 8443}
	lc := NewLocalConfiguration(args, createTestLocalLogger())
	for _, s := range lc.createDefaultStorageConfig() {
		if s.Name == "http" && s.Params["bind_address"] != "127.0.0.1" {
			t.Errorf("bind_address = %v, want 127.0.0.1", s.Params["bind_address"])
		}
	}
}

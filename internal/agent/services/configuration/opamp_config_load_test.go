package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// The block goes through the same loader, substitution and redaction as
// the rest of agent.yaml: a token given by reference is resolved for the
// client and masked by `config show --redact`.
func TestOpAMPBlockLoadsResolvesAndRedacts(t *testing.T) {
	mp := secret.NewMemoryProvider()
	_ = mp.Set("opamp.token", secret.New("alid_a_FROMSTORE"))
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	yamlText := `config_version: 3
agent:
  key: k
opamp:
  enabled: true
  endpoint: wss://opamp.example/v1/opamp
  token: ${secret:opamp.token}
  transport: http
  tls:
    ca_file: /etc/ca.pem
    insecure_skip_verify: true
  heartbeat_interval: 45s
`
	if err := os.WriteFile(path, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}
	want := OpAMPConfig{Enabled: true, Endpoint: "wss://opamp.example/v1/opamp", Token: "alid_a_FROMSTORE", Transport: "http",
		TLS: OpAMPTLSConfig{CAFile: "/etc/ca.pem", InsecureSkipVerify: true}, HeartbeatInterval: "45s"}
	if data.OpAMP == nil || *data.OpAMP != want {
		t.Fatalf("loaded opamp block = %+v, want %+v", data.OpAMP, want)
	}

	shown, err := LoadForShow(path, ShowRedact, nil)
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	out, err := MarshalSortedYAML(&shown)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "alid_a_FROMSTORE") || !strings.Contains(string(out), "token: '***'") {
		t.Errorf("config show --redact does not mask the opamp token:\n%s", out)
	}

	if err := os.WriteFile(path, []byte("config_version: 3\nagent:\n  key: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := LoadFromDisk(path, nil); err != nil || data.OpAMP != nil {
		t.Errorf("absent block: OpAMP = %+v, err = %v; want nil, nil", data.OpAMP, err)
	}
}

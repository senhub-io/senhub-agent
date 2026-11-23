package entitydetect

import (
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func otlpStorage(signals map[string]interface{}) []configuration.StorageConfig {
	return []configuration.StorageConfig{{Name: "otlp", Params: map[string]interface{}{"signals": signals}}}
}

func TestInboundCallersResolve(t *testing.T) {
	logsOff := otlpStorage(map[string]interface{}{"logs": map[string]interface{}{"enabled": false}})
	cases := []struct {
		name    string
		global  *configuration.EntitiesConfig
		storage []configuration.StorageConfig
		want    bool
		mode    string
	}{
		{"absent block is off", &configuration.EntitiesConfig{Enabled: true}, otlpStorage(nil), false, ""},
		{"explicitly off", &configuration.EntitiesConfig{Enabled: true, InboundCallers: &configuration.EntitiesInboundCallersConfig{}}, otlpStorage(nil), false, ""},
		{"on with an OTLP output", &configuration.EntitiesConfig{Enabled: true, InboundCallers: &configuration.EntitiesInboundCallersConfig{Enabled: true, PublicAddresses: "truncated"}}, otlpStorage(nil), true, "truncated"},
		{"on but the logs signal is off", &configuration.EntitiesConfig{Enabled: true, InboundCallers: &configuration.EntitiesInboundCallersConfig{Enabled: true}}, logsOff, false, ""},
		{"on but no OTLP output", &configuration.EntitiesConfig{Enabled: true, InboundCallers: &configuration.EntitiesInboundCallersConfig{Enabled: true}}, nil, false, ""},
		{"no global block", nil, otlpStorage(nil), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Resolve(tc.global, tc.storage, "agent-1")
			if cfg.InboundCallersEnabled != tc.want {
				t.Errorf("InboundCallersEnabled = %v, want %v", cfg.InboundCallersEnabled, tc.want)
			}
			if tc.want && cfg.InboundPublicAddresses != tc.mode {
				t.Errorf("InboundPublicAddresses = %q, want %q", cfg.InboundPublicAddresses, tc.mode)
			}
			if cfg.DependsOnEnabled {
				t.Error("the inbound report must not switch depends_on on")
			}
		})
	}
}

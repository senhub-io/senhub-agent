package app

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

// A valid OTLP output with nothing turning entity emission on used to
// print `[OK] Storage: otlp` and nothing else, and the host was absent
// from every topology built on the rail (#938).
func TestConfigCheckSaysWhenNoEntityEventWillBeEmitted(t *testing.T) {
	otlp := func(params map[string]interface{}) []configuration.StorageConfig {
		return []configuration.StorageConfig{{Name: "otlp", Params: params}}
	}
	cases := []struct {
		name     string
		entities *configuration.EntitiesConfig
		storage  []configuration.StorageConfig
		want     bool
	}{
		{"otlp with no switch at all", nil, otlp(map[string]interface{}{"endpoint": "x"}), true},
		{"otlp with signals.entities off", nil, otlp(map[string]interface{}{
			"signals": map[string]interface{}{"entities": map[string]interface{}{"enabled": false}}}), true},
		{"otlp with signals.entities on", nil, otlp(map[string]interface{}{
			"signals": map[string]interface{}{"entities": map[string]interface{}{"enabled": true}}}), false},
		{"global entities block on", &configuration.EntitiesConfig{Enabled: true}, otlp(nil), false},
		{"no otlp output", nil, []configuration.StorageConfig{{Name: "http"}}, false},
	}
	for _, c := range cases {
		var noted bool
		out := captureStdout(t, func() { noted = reportEntityEmission(c.entities, c.storage) })
		if noted != c.want {
			t.Errorf("%s: noted = %v, want %v", c.name, noted, c.want)
		}
		if noted && !strings.Contains(out, "signals.entities.enabled") {
			t.Errorf("%s: the note does not name the switch:\n%s", c.name, out)
		}
		if !noted && out != "" {
			t.Errorf("%s: printed although nothing to say:\n%s", c.name, out)
		}
	}
}

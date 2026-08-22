package kafka

import "testing"

// TestNumericConfigSurvivesItsDecodedType is the reason #831 exists.
//
// A YAML scalar reaches the probe as int, int64 or float64 depending on
// which loader path it took — the multi-file merge, a JSON round-trip
// through the config API, or a raw yaml.v2 decode. A bare
// `raw["timeout"].(int)` matches exactly one of those and silently
// returns the zero value for the others: no error, no warning, and the
// probe quietly runs on its default instead of the configured value.
func TestNumericConfigSurvivesItsDecodedType(t *testing.T) {
	cases := map[string]interface{}{
		"int":     45,
		"int64":   int64(45),
		"float64": float64(45),
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := parseConfig(map[string]interface{}{
				"brokers": []interface{}{"127.0.0.1:9092"},
				"timeout": value,
			})
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if got := int(cfg.Timeout.Seconds()); got != 45 {
				t.Errorf("timeout decoded from %s = %ds, want 45s — the configured value was dropped", name, got)
			}
		})
	}
}

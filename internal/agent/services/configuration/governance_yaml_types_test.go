package configuration

import (
	"encoding/json"
	"testing"
)

func TestFixYAMLTypesNormalisesTheGovernanceBlock(t *testing.T) {
	lc := &LocalConfiguration{}
	cfg := LocalConfigurationData{Probes: []ProbeConfig{{
		Name: "db", Type: "mysql",
		Params: map[string]interface{}{"host": "h"},
		Governance: map[string]interface{}{
			"criticality": "high",
			"labels":      map[interface{}]interface{}{"application": "erp"},
		},
	}}}
	fixed := lc.fixYAMLTypes(cfg)
	if _, err := json.Marshal(fixed.Probes[0].Governance); err != nil {
		t.Fatalf("governance must be JSON-encodable after the fix: %v", err)
	}
	labels, ok := fixed.Probes[0].Governance["labels"].(map[string]interface{})
	if !ok || labels["application"] != "erp" {
		t.Fatalf("nested labels not normalised: %#v", fixed.Probes[0].Governance["labels"])
	}
}

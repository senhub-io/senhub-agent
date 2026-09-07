package http

import "testing"

func TestCheckGovernanceBlock(t *testing.T) {
	if err := checkGovernanceBlock(nil); err != nil {
		t.Fatalf("nil must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{}); err != nil {
		t.Fatalf("empty must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{
		"criticality": "high", "labels": map[string]interface{}{"application": "erp"},
	}); err != nil {
		t.Fatalf("a documented block must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{"colour": "red"}); err == nil {
		t.Error("an unknown key must be refused")
	}
	if err := checkGovernanceBlock(map[string]interface{}{"criticality": "urgent"}); err == nil {
		t.Error("a value outside the closed set must be refused")
	}
}

func TestProbeWriteRequestToConfigDropsAnEmptyGovernance(t *testing.T) {
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", Governance: map[string]interface{}{}}).toConfig(); cfg.Governance != nil {
		t.Error("an empty object must not be written to the fragment")
	}
	gov := map[string]interface{}{"criticality": "low"}
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", Governance: gov}).toConfig(); cfg.Governance["criticality"] != "low" {
		t.Error("a filled block must be carried to the fragment")
	}
}

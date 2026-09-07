package data_store

import (
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestOperatorTagsForProbe(t *testing.T) {
	if got, err := operatorTagsForProbe(configuration.ProbeConfig{Name: "cpu"}); err != nil || got != nil {
		t.Fatalf("nothing declared must give nil, got %v, %v", got, err)
	}
	got, err := operatorTagsForProbe(configuration.ProbeConfig{
		Name:       "db",
		CustomTags: map[string]string{"env": "prod", "service.criticality": "override"},
		Governance: map[string]interface{}{
			"criticality": "high",
			"owner":       map[string]interface{}{"team": "dba"},
			"labels":      map[string]interface{}{"application": "erp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"env":                      "prod",
		"service.criticality":      "override",
		"entity.owner.team":        "dba",
		"entity.label.application": "erp",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, err := operatorTagsForProbe(configuration.ProbeConfig{Name: "db", Governance: map[string]interface{}{"criticality": "urgent"}}); err == nil {
		t.Error("an invalid block must be an error, not a silent drop")
	}
}

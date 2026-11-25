package agentmetrics

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/license"
)

func TestBuildAgentRecords_LicenseExpiresIn(t *testing.T) {
	license.ResetCurrentForTest()
	t.Cleanup(license.ResetCurrentForTest)

	for _, r := range BuildAgentRecords(AgentMetricsSnapshot{StartTime: time.Now()}) {
		if r.Name == "senhub.agent.license.expires_in" {
			t.Fatal("gauge emitted before anything was published")
		}
	}

	license.Publish(license.InForce{
		State:   license.StateGrace,
		License: &license.License{ExpiresAt: time.Now().Add(-time.Hour), IsExpired: true, Platform: "senhub"},
	})

	found := false
	for _, r := range BuildAgentRecords(AgentMetricsSnapshot{StartTime: time.Now()}) {
		if r.Name != "senhub.agent.license.expires_in" {
			continue
		}
		found = true
		if r.Type != "gauge" {
			t.Errorf("type: got %q, want gauge", r.Type)
		}
		if r.Unit != "s" {
			t.Errorf("unit: got %q, want s", r.Unit)
		}
		if r.Value >= 0 {
			t.Errorf("value: got %v, want negative (grace)", r.Value)
		}
		if r.Attributes["state"] != string(license.StateGrace) {
			t.Errorf("state attribute: got %q, want grace", r.Attributes["state"])
		}
		if r.Attributes["platform"] != "senhub" {
			t.Errorf("platform attribute: got %q, want senhub", r.Attributes["platform"])
		}
	}
	if !found {
		t.Fatal("gauge absent after publication of a grace licence")
	}
}

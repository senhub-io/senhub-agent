package agentmetrics

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

func findUpdateRecord(t *testing.T, records []otelmapper.OtelRecord, name string) *otelmapper.OtelRecord {
	t.Helper()
	for i := range records {
		if records[i].Name == name {
			return &records[i]
		}
	}
	return nil
}

func TestUpdateSettingMetrics(t *testing.T) {
	agentstate.ResetUpdateSettingForTest()
	t.Cleanup(agentstate.ResetUpdateSettingForTest)

	records := BuildAgentRecords(AgentMetricsSnapshot{})
	if r := findUpdateRecord(t, records, "senhub.agent.update.info"); r != nil {
		t.Fatal("senhub.agent.update.info exists before the updater published")
	}
	if r := findUpdateRecord(t, records, "senhub.agent.update.paused"); r != nil {
		t.Fatal("senhub.agent.update.paused exists before the updater published")
	}

	agentstate.SetUpdateSetting(agentstate.UpdateSetting{
		Enabled:     true,
		IncludeBeta: false,
		Target:      "latest",
		Source:      "server",
		Paused:      true,
	})

	records = BuildAgentRecords(AgentMetricsSnapshot{})
	info := findUpdateRecord(t, records, "senhub.agent.update.info")
	if info == nil {
		t.Fatal("senhub.agent.update.info missing after publish")
	}
	wantAttrs := map[string]string{
		"enabled":      "true",
		"include_beta": "false",
		"target":       "latest",
		"source":       "server",
	}
	for k, v := range wantAttrs {
		if info.Attributes[k] != v {
			t.Errorf("info attribute %s = %q, want %q", k, info.Attributes[k], v)
		}
	}
	if info.Value != 1 {
		t.Errorf("info value = %v, want 1", info.Value)
	}

	paused := findUpdateRecord(t, records, "senhub.agent.update.paused")
	if paused == nil {
		t.Fatal("senhub.agent.update.paused missing after publish")
	}
	if paused.Value != 1 {
		t.Errorf("paused value = %v, want 1", paused.Value)
	}
	if len(paused.Attributes) != 0 {
		t.Errorf("paused has attributes: %v", paused.Attributes)
	}
	if paused.Unit != "{status}" {
		t.Errorf("paused unit = %q, want {status}", paused.Unit)
	}
}

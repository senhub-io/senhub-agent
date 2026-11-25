package statuschan

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/status"
)

func TestUpdateInfoFrom(t *testing.T) {
	agentstate.ResetUpdateSettingForTest()
	t.Cleanup(agentstate.ResetUpdateSettingForTest)

	if got := updateInfoFrom(agentstate.UpdateSetting{Target: "latest"}); got.Target != "latest" {
		t.Fatalf("Target = %q, want latest", got.Target)
	}

	agentstate.SetUpdateSetting(agentstate.UpdateSetting{
		Enabled:     true,
		IncludeBeta: false,
		Target:      "latest",
		Source:      "server",
		Paused:      true,
	})
	s, ok := agentstate.GetUpdateSetting()
	if !ok {
		t.Fatal("setting not published")
	}
	got := updateInfoFrom(s)
	want := status.UpdateInfo{
		Enabled:     true,
		IncludeBeta: false,
		Target:      "latest",
		Source:      "server",
		Paused:      true,
	}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

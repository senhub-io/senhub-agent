package status

import (
	"strings"
	"testing"
)

func TestAgentInfoShowsTheInstanceID(t *testing.T) {
	f := &CLIFormatter{}
	out := f.formatAgentInfo(AgentInfo{Version: "0.6.0", InstanceID: "4fa46ab7-7905-576b-b737-d47efd7b0556"})
	if !strings.Contains(out, "Instance:   4fa46ab7-7905-576b-b737-d47efd7b0556") {
		t.Errorf("status does not show the instance id:\n%s", out)
	}
	if strings.Contains(f.formatAgentInfo(AgentInfo{Version: "0.6.0"}), "Instance:") {
		t.Error("an agent that reports no instance id shows an empty Instance line")
	}
}

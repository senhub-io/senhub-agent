package http

import (
	"strings"
	"testing"
)

// The console runs on the administration key. Settings showed that key
// under "Agent key", with the text telling the operator to give it to
// Sensor Factory to order a licence, and its Copy button copied it: a
// licence ordered that way is bound to the wrong key.
func TestSettingsShowsTheAgentKeyNotTheAdministrationKey(t *testing.T) {
	const adminKey = "319a2cf8-7a69-4752-b183-dd4f8430c10d"
	const agentKey = "50327df5-35b0-4d3c-8288-a99ffbca544f"
	html, err := NewAssetHandler(adminKey).WithReadKey(agentKey).RenderTemplate("settings")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(html, `id="agentkey"`)
	if i < 0 {
		t.Fatal("no agent key box on the settings page")
	}
	box := html[i : i+200]
	if !strings.Contains(box, agentKey) || strings.Contains(box, adminKey) {
		t.Errorf("agent key box = %q, want the agent key and not the administration key", box)
	}
	if strings.Contains(html, "writeText(window.AGENT_KEY)") {
		t.Error("the Copy button copies the administration key")
	}
}

package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAgentGovernancePreservesTheFileAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	src := "# the agent\nconfig_version: 3\nagent:\n  key: \"k\" # identity\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetAgentGovernance(path, map[string]interface{}{
		"criticality": "high",
		"owner":       map[string]interface{}{"team": "sre"},
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), "# the agent\n") || !strings.Contains(string(raw), "# identity") {
		t.Errorf("comments must survive:\n%s", raw)
	}

	data, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := data.Governance["owner"].(map[string]interface{})
	if data.Governance["criticality"] != "high" || owner["team"] != "sre" {
		t.Errorf("the loader must expose the block with string keys: %#v", data.Governance)
	}

	if err := SetAgentGovernance(path, nil); err != nil {
		t.Fatal(err)
	}
	if raw, _ = os.ReadFile(path); strings.Contains(string(raw), "governance") {
		t.Errorf("nil must remove the block:\n%s", raw)
	}
}

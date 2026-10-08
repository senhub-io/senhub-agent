package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentGovernanceAPI_RoundTrip(t *testing.T) {
	router, dir := newOutputsTestRouter(t)
	url := "/api/" + testAdminKey + "/config/governance"
	agentYAML := filepath.Join(dir, "agent.yaml")

	code, got := doJSON(t, router, "GET", url, nil)
	if code != 200 {
		t.Fatalf("GET: %d %v", code, got)
	}
	if gov, _ := got["governance"].(map[string]interface{}); len(gov) != 0 || got["read_only"] != false {
		t.Fatalf("a file without governance must read as empty and writable: %v", got)
	}

	body := map[string]interface{}{"governance": map[string]interface{}{
		"owner":       map[string]interface{}{"team": "sre", "contact": " sre@example.com "},
		"criticality": "high",
		"lifecycle":   "active",
		"location":    map[string]interface{}{"site": "paris", "rack": ""},
		"labels":      map[string]interface{}{"environment": "staging"},
	}}
	if code, resp := doJSON(t, router, "PUT", url, body); code != 200 {
		t.Fatalf("PUT: %d %v", code, resp)
	}

	raw, err := os.ReadFile(agentYAML)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "agent:\n  key: \"k\"") || !strings.Contains(string(raw), "governance:") {
		t.Errorf("agent.yaml must keep its blocks and gain governance:\n%s", raw)
	}
	if strings.Contains(string(raw), "rack") {
		t.Errorf("an empty field must not be written:\n%s", raw)
	}
	if strings.Contains(string(raw), "probes:") || strings.Contains(string(raw), "storage:") {
		t.Errorf("a write must not flip the multi-file layout:\n%s", raw)
	}

	code, got = doJSON(t, router, "GET", url, nil)
	gov, _ := got["governance"].(map[string]interface{})
	owner, _ := gov["owner"].(map[string]interface{})
	if code != 200 || gov["criticality"] != "high" || owner["contact"] != "sre@example.com" {
		t.Fatalf("GET after PUT must return what was saved, trimmed: %d %v", code, got)
	}

	if code, resp := doJSON(t, router, "PUT", url, map[string]interface{}{"governance": map[string]interface{}{}}); code != 200 {
		t.Fatalf("clearing: %d %v", code, resp)
	}
	raw, _ = os.ReadFile(agentYAML)
	if strings.Contains(string(raw), "governance") {
		t.Errorf("an empty form must remove the block:\n%s", raw)
	}
}

func TestAgentGovernanceAPI_InvalidValuesNameTheField(t *testing.T) {
	router, dir := newOutputsTestRouter(t)
	url := "/api/" + testAdminKey + "/config/governance"
	before, _ := os.ReadFile(filepath.Join(dir, "agent.yaml"))

	code, resp := doJSON(t, router, "PUT", url, map[string]interface{}{"governance": map[string]interface{}{"criticality": "urgent"}})
	fields, _ := resp["fields"].(map[string]interface{})
	if code != 400 || fields["governance.criticality"] == nil {
		t.Fatalf("a criticality outside the set: %d %v, want 400 naming governance.criticality", code, resp)
	}

	code, resp = doJSON(t, router, "PUT", url, map[string]interface{}{"governance": map[string]interface{}{"colour": "red"}})
	fields, _ = resp["fields"].(map[string]interface{})
	if code != 400 || fields["governance.colour"] == nil {
		t.Fatalf("an unknown key: %d %v, want 400 naming governance.colour", code, resp)
	}

	after, _ := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if string(before) != string(after) {
		t.Errorf("a refused write must leave the file alone:\n%s", after)
	}
}

func TestAgentGovernanceAPI_ReferencesAreReadOnly(t *testing.T) {
	router, dir := newOutputsTestRouter(t)
	url := "/api/" + testAdminKey + "/config/governance"
	content := "config_version: 3\nagent:\n  key: \"k\"\ngovernance:\n  criticality: ${env:SITE_CRIT:-low}\n  owner:\n    team: sre\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	code, got := doJSON(t, router, "GET", url, nil)
	if code != 200 || got["read_only"] != true || !strings.Contains(got["reason"].(string), "(criticality)") {
		t.Fatalf("GET: %d %v, want read_only with the reason naming the field", code, got)
	}

	code, resp := doJSON(t, router, "PUT", url, map[string]interface{}{"governance": map[string]interface{}{"criticality": "high"}})
	if code != 409 {
		t.Fatalf("PUT: %d %v, want 409", code, resp)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if string(after) != content {
		t.Errorf("a refused write must leave the file alone:\n%s", after)
	}
}

package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The console is opened with the administration key, and it named that
// key "Agent key" on its licence card and in the settings API, which also
// checked licence binding against it. The agent key is the configured
// one: a monitoring tool reads with it and a licence is bound to it.
func TestTheConsoleShowsTheAgentKeyNotTheAdministrationKey(t *testing.T) {
	router := surfaceRouter(t, map[string]interface{}{
		"endpoints": []interface{}{"prtg", "web"},
		"admin_key": "admin-key",
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/admin-key/config/settings", nil))
	var got struct {
		AgentKey string `json:"agent_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("settings answer: %v (%s)", err, rec.Body.String())
	}
	if got.AgentKey != "read-key" {
		t.Errorf("settings agent_key = %q, want the agent key", got.AgentKey)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/web/admin-key/dashboard", nil))
	if !strings.Contains(rec.Body.String(), "window.AGENT_READ_KEY = 'read-key'") {
		t.Error("the dashboard page does not carry the agent key for its licence card")
	}
}

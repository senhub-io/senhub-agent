package http

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

// The system info names the agent the way its telemetry and its entity
// do, so an operator can find it downstream; it is the derived id, never
// the key.
func TestSystemInfoCarriesTheInstanceID(t *testing.T) {
	router := surfaceRouter(t, map[string]interface{}{"endpoints": []interface{}{"prtg"}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/read-key/info/system", nil))
	var got struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %d %s: %v", rec.Code, rec.Body.String(), err)
	}
	if want := configuration.AgentInstanceID("read-key"); got.InstanceID != want {
		t.Errorf("instance_id = %q, want %q", got.InstanceID, want)
	}
	if strings.Contains(rec.Body.String(), `"instance_id":"read-key"`) {
		t.Error("instance_id is the key")
	}
}

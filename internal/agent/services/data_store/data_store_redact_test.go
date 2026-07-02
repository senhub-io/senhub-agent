package data_store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestRetrieveOrCreate_RedactsParamsInLog is the data-store call-site
// regression: when a strategy's parameters change and the store logs the
// old/new params while attempting an update, resolved credentials must be
// masked rather than echoed in cleartext.
func TestRetrieveOrCreate_RedactsParamsInLog(t *testing.T) {
	const oldSecret = "old-bearer-should-not-leak"
	const newSecret = "new-bearer-should-not-leak"

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	base := (*logger.Logger)(&zl)

	mockConfig := &MockAgentConfig{authKey: "test-key"}
	mockProvider := &MockConfigProvider{}
	ds := NewDataStore(mockConfig, mockProvider, base).(*dataStore)

	// Seed an active strategy of the same name but with a secret-bearing
	// parameter, so a differing incoming config drives the update log path.
	existing := &MockStrategy{
		name:   "otlp",
		params: map[string]interface{}{"bearer_token": oldSecret, "endpoint": "collector:4317"},
	}
	active := []SyncStrategy{existing}
	ds.strategies.Store(&active)

	incoming := configuration.StorageConfig{
		Name:   "otlp",
		Params: map[string]interface{}{"bearer_token": newSecret, "endpoint": "collector:9999"},
	}

	// MockStrategy is not an *http.HTTPSyncStrategy, so this exercises the
	// "changed params, will recreate" branch that logs old/new params.
	_ = ds.retrieveOrCreate(incoming)

	out := buf.String()
	if strings.Contains(out, oldSecret) {
		t.Errorf("retrieveOrCreate log leaked old bearer token:\n%s", out)
	}
	if strings.Contains(out, newSecret) {
		t.Errorf("retrieveOrCreate log leaked new bearer token:\n%s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("retrieveOrCreate log should contain masked marker ***; got:\n%s", out)
	}
}

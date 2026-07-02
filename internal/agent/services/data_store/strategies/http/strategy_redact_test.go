package http

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

// TestUpdateConfiguration_RedactsParamsInLog is the HTTP-strategy call-site
// regression: the runtime reconfiguration log line must not echo resolved
// credentials carried in the incoming params.
func TestUpdateConfiguration_RedactsParamsInLog(t *testing.T) {
	const secret = "api-token-should-not-leak"

	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	base := (*logger.Logger)(&zl)

	agentConfig := createTestAgentConfig()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, base).(*HTTPSyncStrategy)

	// No port/bind_address keys, so no server restart is triggered.
	newParams := map[string]interface{}{
		"api_token": secret,
	}
	if err := strategy.UpdateConfiguration(newParams); err != nil {
		t.Fatalf("UpdateConfiguration: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("UpdateConfiguration log leaked api_token in cleartext:\n%s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("UpdateConfiguration log should contain masked marker ***; got:\n%s", out)
	}
}

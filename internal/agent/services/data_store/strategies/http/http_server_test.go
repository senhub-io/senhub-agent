package http

import (
	"net"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

func newServerTestStrategy(t *testing.T, port int) *HTTPSyncStrategy {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	agentConfig := configuration.NewAgentConfiguration("test-agent-key", "http://test-server.example", baseLogger)
	params := map[string]interface{}{
		"port":         port,
		"bind_address": "127.0.0.1",
	}
	strategy, ok := NewHTTPSyncStrategy(agentConfig, params, baseLogger).(*HTTPSyncStrategy)
	if !ok {
		t.Fatal("failed to cast strategy")
	}
	return strategy
}

// TestServerManager_StartFailsOnTakenPort pins the #273 fix: a bind
// failure aborts Start with an error instead of the agent running
// "healthy" with no HTTP surface (no Prometheus scrape, no Web UI, no
// PRTG pull).
func TestServerManager_StartFailsOnTakenPort(t *testing.T) {
	// Occupy a port first.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	strategy := newServerTestStrategy(t, port)
	err = strategy.serverManager.Start()
	if err == nil {
		t.Fatal("Start must fail when the port is already taken")
	}
	if !strings.Contains(err.Error(), "binding HTTP server") {
		t.Errorf("error should name the bind failure, got: %v", err)
	}
}

// TestServerManager_StartServesOnFreePort pins the happy path: a free
// port binds synchronously and Start returns nil.
func TestServerManager_StartServesOnFreePort(t *testing.T) {
	strategy := newServerTestStrategy(t, 0) // kernel-assigned free port
	if err := strategy.serverManager.Start(); err != nil {
		t.Fatalf("Start on a free port: %v", err)
	}
	t.Cleanup(func() {
		_ = strategy.serverManager.GetServer().Close()
	})
}

// TestMetricsProcessor_ReceivesLookupRegistry pins the second #273
// defect: the processor used to be constructed BEFORE the lookup
// registry (it captured nil), so lookup-based status mapping silently
// never ran.
func TestMetricsProcessor_ReceivesLookupRegistry(t *testing.T) {
	strategy := newServerTestStrategy(t, 0)
	if strategy.lookupRegistry == nil {
		t.Skip("lookup registry unavailable in this environment")
	}
	if strategy.metricsProcessor.lookupRegistry == nil {
		t.Fatal("metrics processor captured a nil lookup registry (construction-order regression)")
	}
}

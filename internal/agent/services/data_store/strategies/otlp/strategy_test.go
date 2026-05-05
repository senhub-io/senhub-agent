package otlp

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// fakeAgentConfig satisfies the bits of AgentConfiguration that the
// OTLP strategy actually reads (just GetAuthenticationKey for now).
// We avoid pulling in the real LocalConfiguration to keep the test
// focused on the strategy itself.
type fakeAgentConfig struct {
	configuration.AgentConfiguration
	key string
}

func (f *fakeAgentConfig) GetAuthenticationKey() string { return f.key }

func newTestStrategy(t *testing.T, params map[string]interface{}) *OTLPSyncStrategy {
	t.Helper()
	if params == nil {
		params = map[string]interface{}{
			"endpoint": "127.0.0.1:65000", // unreachable port, never dialed (lazy)
			"tls": map[string]interface{}{
				"enabled": false,
			},
		}
	}
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	got := NewOTLPSyncStrategy(&fakeAgentConfig{key: "test-key-12345678-abcdef"}, params, baseLogger)
	s, ok := got.(*OTLPSyncStrategy)
	if !ok {
		t.Fatalf("constructor returned %T, want *OTLPSyncStrategy", got)
	}
	return s
}

func TestStrategy_NameAndParams(t *testing.T) {
	params := map[string]interface{}{
		"endpoint": "x:4317",
		"tls":      map[string]interface{}{"enabled": false},
	}
	s := newTestStrategy(t, params)
	if got := s.GetStrategyName(); got != "otlp" {
		t.Errorf("name=%q", got)
	}
	got := s.GetStrategyParams()
	if got["endpoint"] != "x:4317" {
		t.Errorf("params not preserved: %v", got)
	}
}

func TestStrategy_ValidateConfigParams(t *testing.T) {
	s := newTestStrategy(t, nil)

	// Valid params.
	if err := s.ValidateConfigParams(map[string]interface{}{
		"endpoint": "x:4317",
		"tls":      map[string]interface{}{"enabled": false},
	}); err != nil {
		t.Errorf("valid params rejected: %v", err)
	}

	// Missing endpoint.
	if err := s.ValidateConfigParams(map[string]interface{}{}); err == nil {
		t.Errorf("missing endpoint accepted")
	}
}

func TestStrategy_StartShutdown(t *testing.T) {
	s := newTestStrategy(t, nil)

	if err := s.Start(); err != nil {
		t.Fatalf("Start returned: %v", err)
	}
	if !s.started {
		t.Errorf("started flag not set")
	}
	if s.exporters == nil || s.exporters.metric == nil || s.exporters.log == nil {
		t.Errorf("exporters not built: %+v", s.exporters)
	}

	// Idempotent start.
	if err := s.Start(); err != nil {
		t.Errorf("second Start returned: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned: %v", err)
	}
	if !s.shutdown {
		t.Errorf("shutdown flag not set")
	}

	// Idempotent shutdown.
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown returned: %v", err)
	}

	// Cannot restart after shutdown.
	if err := s.Start(); err == nil {
		t.Errorf("Start after Shutdown should fail")
	}
}

func TestStrategy_AddDataPointsCounts(t *testing.T) {
	s := newTestStrategy(t, nil)
	dps := []datapoint.DataPoint{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	if err := s.AddDataPoints(dps); err != nil {
		t.Fatalf("AddDataPoints err: %v", err)
	}
	if s.dataPointsSeen != 3 {
		t.Errorf("dataPointsSeen=%d, want 3", s.dataPointsSeen)
	}
	if err := s.AddDataPoints(dps); err != nil {
		t.Fatal(err)
	}
	if s.dataPointsSeen != 6 {
		t.Errorf("dataPointsSeen=%d, want 6", s.dataPointsSeen)
	}
}

func TestStrategy_DefaultsServiceInstance(t *testing.T) {
	// service.instance.id should default to first 8 chars of agent key
	// when not overridden.
	s := newTestStrategy(t, nil)
	if got := s.cfg.Resource.ServiceInstance; got != "test-key" {
		t.Errorf("ServiceInstance=%q, want %q", got, "test-key")
	}
}

func TestStrategy_ShutdownWithoutStart(t *testing.T) {
	// Shutting down a strategy that was never started should not panic
	// and should be a clean no-op.
	s := newTestStrategy(t, nil)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown without Start: %v", err)
	}
}

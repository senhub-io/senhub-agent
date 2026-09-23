package entitydetect

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// The point of moving the detector out of the OTLP strategy: an agent
// with no OTLP output must still produce entity events, because that is
// what lets any other output subscribe to them (#932).
func TestDetectionRunsWithoutAnyOTLPOutput(t *testing.T) {
	events := entity.SubscribeEvents(16)

	svc := New(Config{
		Enabled:         true,
		Interval:        50 * time.Millisecond,
		AgentInstanceID: "agent-under-test",
		AgentService:    "senhub-agent",
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()

	select {
	case ev := <-events:
		if ev.Entity == nil {
			t.Error("an event arrived carrying no entity")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no entity event was produced without an OTLP output configured")
	}
}

func TestDisabledDetectionCostsNothing(t *testing.T) {
	svc := New(Config{Enabled: false}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(svc.unregisters) != 0 {
		t.Errorf("disabled detection registered %d source(s)", len(svc.unregisters))
	}
}

// An existing install configures entities under its OTLP output. Reading
// that must keep working, or upgrading would silently stop the rail.
func TestResolveFallsBackToWhatAnOTLPOutputDeclares(t *testing.T) {
	storage := []configuration.StorageConfig{
		{Name: "zabbix", Params: map[string]interface{}{"server": "127.0.0.1:10051"}},
		{Name: "otlp", Params: map[string]interface{}{
			"signals": map[string]interface{}{
				"entities": map[string]interface{}{
					"enabled":                  true,
					"interval":                 "90s",
					"depends_on_enabled":       true,
					"depends_on_debounce":      5,
					"depends_on_exclude_cidrs": []interface{}{"10.0.0.0/8", "pas-un-cidr"},
				},
			},
		}},
	}

	cfg := Resolve(nil, storage, "agent-1")
	if !cfg.Enabled {
		t.Fatal("an OTLP output with entities enabled did not turn detection on")
	}
	if cfg.Interval != 90*time.Second {
		t.Errorf("interval = %v; want 90s", cfg.Interval)
	}
	if !cfg.DependsOnEnabled || cfg.DependsOnDebounce != 5 {
		t.Errorf("depends_on = %v / debounce %d", cfg.DependsOnEnabled, cfg.DependsOnDebounce)
	}
	// One entry parses, one does not: a typo in a privacy filter must
	// not take detection down with it.
	if len(cfg.DependsOnExcludeCIDRs) != 1 {
		t.Errorf("kept %d CIDR(s); want the one that parses", len(cfg.DependsOnExcludeCIDRs))
	}
}

// The global block is the operator speaking directly, so it wins over
// whatever an output happens to declare.
func TestGlobalBlockOverridesTheOutput(t *testing.T) {
	storage := []configuration.StorageConfig{
		{Name: "otlp", Params: map[string]interface{}{
			"signals": map[string]interface{}{
				"entities": map[string]interface{}{"enabled": true, "interval": "90s"},
			},
		}},
	}
	cfg := Resolve(&configuration.EntitiesConfig{Enabled: true, Interval: "30s"}, storage, "agent-1")
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v; want the global block's 30s", cfg.Interval)
	}
}

func TestNeitherSourceMeansNoDetection(t *testing.T) {
	cfg := Resolve(nil, []configuration.StorageConfig{
		{Name: "zabbix", Params: map[string]interface{}{"server": "x"}},
	}, "agent-1")
	if cfg.Enabled {
		t.Error("detection turned itself on with nothing asking for it")
	}
}

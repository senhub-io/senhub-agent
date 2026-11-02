package sensor

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func init() {
	probes.RegisterProbe("parallel_slow_test", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		cpu, _ := probes.LookupProbeConstructor("cpu")
		p, err := cpu(params, l)
		if err != nil {
			return nil, err
		}
		return slowFirstCollect{Probe: p}, nil
	})
	// Registered under a listener type so Phase 3 sorts it first; the fake
	// constructor itself is instant.
	probes.RegisterProbe("otlp_receiver", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		cpu, _ := probes.LookupProbeConstructor("cpu")
		return cpu(params, l)
	})
	probes.RegisterProbe("parallel_fail_test", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		return nil, errors.New("parallel start failure")
	})
}

// slowFirstCollect stands for a probe whose OnStart blocks, as one that
// connects to its target before the first cycle does.
type slowFirstCollect struct{ types.Probe }

func (p slowFirstCollect) OnStart(stop chan struct{}) error {
	time.Sleep(300 * time.Millisecond)
	return p.Probe.OnStart(stop)
}

func syncWith(t *testing.T, cfgs []configuration.ProbeConfig) (*sensor, time.Duration) {
	t.Helper()
	provider := &MockConfigProvider{config: configuration.ConfigurationData{Probes: cfgs}}
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	s.licenseValidator = &fakeLicenseValidator{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.runCtx = ctx
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	start := time.Now()
	if err := s.SyncConfiguration(); err != nil {
		t.Fatal(err)
	}
	return s, time.Since(start)
}

// Four slow starts must overlap: run in parallel they take about as long
// as one, where a serial start took four times as long.
func TestSyncConfigurationStartsProbesInParallel(t *testing.T) {
	slow := func(name string) configuration.ProbeConfig {
		return configuration.ProbeConfig{Name: name, Type: "parallel_slow_test", Params: map[string]interface{}{"interval": 30}}
	}
	_, one := syncWith(t, []configuration.ProbeConfig{slow("alone")})

	cfgs := []configuration.ProbeConfig{
		slow("slow-1"), slow("slow-2"), slow("slow-3"), slow("slow-4"),
		{Name: "listener", Type: "otlp_receiver", Params: map[string]interface{}{"interval": 30}},
		{Name: "fails", Type: "parallel_fail_test", Params: map[string]interface{}{"interval": 30}},
	}
	s, four := syncWith(t, cfgs)
	if four > 2*one {
		t.Errorf("four slow starts took %v, one took %v: they did not overlap", four, one)
	}
	if len(s.startedProbes) != 5 {
		t.Errorf("startedProbes = %d, want 5 (4 slow + listener)", len(s.startedProbes))
	}
	if _, ok := s.failedProbes[probes.GenerateProbeId(cfgs[5])]; !ok {
		t.Error("the failing probe is not in failedProbes")
	}
}

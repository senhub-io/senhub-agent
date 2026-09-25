package sensor

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var flakyStartFails atomic.Bool

func init() {
	probes.RegisterProbe("flaky_start_test", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		if flakyStartFails.Load() {
			return nil, errors.New("connect: Password is expired")
		}
		cpu, _ := probes.LookupProbeConstructor("cpu")
		return cpu(params, l)
	})
}

// A probe whose start fails is not a silent one: it counts in the total
// and never as healthy, the console shows why, and it comes back by itself
// once the cause is fixed. On a dev host an ibmi probe whose password had
// expired stayed absent for eight days while the agent reported nine
// healthy probes out of nine.
func TestAProbeThatCannotStartIsCountedShownAndRetried(t *testing.T) {
	flakyStartFails.Store(true)
	defer flakyStartFails.Store(false)

	cfg := configuration.ProbeConfig{Name: "ibmi-like", Type: "flaky_start_test", Params: map[string]interface{}{"interval": 30}}
	provider := &MockConfigProvider{config: configuration.ConfigurationData{Probes: []configuration.ProbeConfig{cfg}}}
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	s.licenseValidator = &fakeLicenseValidator{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	total, healthy := agentstate.GetProbeCounts()
	if total != 1 || healthy != 0 {
		t.Errorf("counts = %d total, %d healthy; a probe that failed to start is configured and not healthy", total, healthy)
	}
	state := agentstate.GetProbeRunState(probes.GenerateProbeId(cfg))
	if state.Health != "failed" || !strings.Contains(state.LastError, "Password is expired") {
		t.Errorf("console state = %+v, want failed with the reason", state)
	}

	flakyStartFails.Store(false)
	s.retryFailedProbesOnce()
	state = agentstate.GetProbeRunState(probes.GenerateProbeId(cfg))
	if !state.Running {
		t.Errorf("the probe did not come back once its cause was fixed: %+v", state)
	}
	if total, _ := agentstate.GetProbeCounts(); total != 1 {
		t.Errorf("total = %d after recovery, want 1", total)
	}
}

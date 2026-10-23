package sensor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// silentTarget stands for an address that accepts nothing and answers
// nothing: every collection waits until the test releases it.
var silentTarget struct {
	mu      sync.Mutex
	release chan struct{}
}

func releaseSilentTarget() {
	silentTarget.mu.Lock()
	defer silentTarget.mu.Unlock()
	if silentTarget.release != nil {
		close(silentTarget.release)
		silentTarget.release = nil
	}
}

func silentTargetGate() chan struct{} {
	silentTarget.mu.Lock()
	defer silentTarget.mu.Unlock()
	if silentTarget.release == nil {
		silentTarget.release = make(chan struct{})
	}
	return silentTarget.release
}

type silentProbe struct {
	types.Probe
	gate chan struct{}
}

func (p silentProbe) Collect() ([]datapoint.DataPoint, error) {
	<-p.gate
	return p.Probe.Collect()
}

func init() {
	probes.RegisterProbe("silent_target_test", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		cpu, _ := probes.LookupProbeConstructor("cpu")
		inner, err := cpu(params, l)
		if err != nil {
			return nil, err
		}
		return silentProbe{Probe: inner, gate: silentTargetGate()}, nil
	})
}

// A probe pointed at a silent address must not hold the sensor: on a
// recette host an snmp_poll probe whose first collection never returned
// kept Start, and every service started after it, waiting, and an
// overlay disabling that probe could not be applied because stopping it
// waited on the same collection.
func TestAProbeOnASilentTargetHoldsNeitherStartNorItsOwnStop(t *testing.T) {
	defer releaseSilentTarget()
	previousBudget := probeStopBudget
	probeStopBudget = 200 * time.Millisecond
	defer func() { probeStopBudget = previousBudget }()

	cfg := configuration.ProbeConfig{Name: "snmp-like", Type: "silent_target_test", Params: map[string]interface{}{"interval": 30}}
	provider := &MockConfigProvider{config: configuration.ConfigurationData{Probes: []configuration.ProbeConfig{cfg}}}
	var delivered atomic.Int32
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error {
		delivered.Add(1)
		return nil
	}
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	s.licenseValidator = &fakeLicenseValidator{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan error, 1)
	go func() { started <- s.Start(ctx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start waited on the probe's first collection")
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	if state := agentstate.GetProbeRunState(probes.GenerateProbeId(cfg)); !state.Running {
		t.Errorf("the probe is not reported running while its first collection waits: %+v", state)
	}

	provider.config.Probes = nil
	stopped := make(chan error, 1)
	go func() { stopped <- s.SyncConfiguration() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopping the probe waited on its collection past the stop budget")
	}
	if len(s.startedProbes) != 0 {
		t.Errorf("%d probes still started after the probe was removed", len(s.startedProbes))
	}

	releaseSilentTarget()
	time.Sleep(200 * time.Millisecond)
	if n := delivered.Load(); n != 0 {
		t.Errorf("a stopped probe delivered %d batches once its collection returned", n)
	}
}

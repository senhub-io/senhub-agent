package sensor

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// The retry goroutine waits on a channel while no probe failed to start:
// a healthy agent holds no timer for it. A failure wakes it, and the timer
// then brings the probe back without a restart.
func TestRetryTimerRunsOnlyWhileAProbeIsWaiting(t *testing.T) {
	flakyStartFails.Store(true)
	defer flakyStartFails.Store(false)
	previous := probeStartRetryInterval
	probeStartRetryInterval = 50 * time.Millisecond
	defer func() { probeStartRetryInterval = previous }()

	cfg := configuration.ProbeConfig{Name: "flaky", Type: "flaky_start_test", Params: map[string]interface{}{"interval": 30}}
	provider := &MockConfigProvider{config: configuration.ConfigurationData{Probes: []configuration.ProbeConfig{cfg}}}
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	s.licenseValidator = &fakeLicenseValidator{}

	if s.hasRetryableFailedProbes() {
		t.Fatal("a sensor that has started nothing reports a probe waiting for a retry")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	if !s.hasRetryableFailedProbes() {
		t.Fatal("a probe that failed to start is not waiting for a retry")
	}

	flakyStartFails.Store(false)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if agentstate.GetProbeRunState(probes.GenerateProbeId(cfg)).Running {
			if s.hasRetryableFailedProbes() {
				t.Error("the recovered probe still counts as waiting for a retry")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the retry timer did not bring the probe back")
}

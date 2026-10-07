package sensor

import (
	"context"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// Governance is not part of a probe's identity: a reload that changes only
// the governance block must keep the running probe and still hand it the new
// attributes, so the next entity emission carries them with no restart.
func TestGovernanceOnlyReloadUpdatesTheRunningProbe(t *testing.T) {
	provider := &MockConfigProvider{}
	withGovernance := func(crit string) []configuration.ProbeConfig {
		return []configuration.ProbeConfig{{
			Name:       "mem",
			Type:       "memory",
			Params:     map[string]interface{}{"interval": 30},
			Governance: map[string]interface{}{"criticality": crit},
		}}
	}
	provider.config.Probes = withGovernance("low")
	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	t.Cleanup(func() {
		for _, p := range s.startedProbes {
			_ = p.Shutdown(context.Background())
		}
	})

	if err := s.SyncConfiguration(); err != nil || len(s.startedProbes) != 1 {
		t.Fatalf("first start: err=%v running=%d", err, len(s.startedProbes))
	}
	first := s.startedProbes[0]
	if got := first.GovernanceAttributes()["service.criticality"]; got != "low" {
		t.Fatalf("initial criticality = %v, want low", got)
	}

	provider.config.Probes = withGovernance("critical")
	if err := s.SyncConfiguration(); err != nil {
		t.Fatal(err)
	}
	if len(s.startedProbes) != 1 || s.startedProbes[0] != first {
		t.Fatal("a governance-only change must keep the same probe instance")
	}
	if got := first.GovernanceAttributes()["service.criticality"]; got != "critical" {
		t.Errorf("criticality after reload = %v, want critical", got)
	}

	provider.config.Probes[0].Governance = nil
	if err := s.SyncConfiguration(); err != nil {
		t.Fatal(err)
	}
	if len(first.GovernanceAttributes()) != 0 {
		t.Errorf("removed governance must clear the attributes, got %v", first.GovernanceAttributes())
	}
}

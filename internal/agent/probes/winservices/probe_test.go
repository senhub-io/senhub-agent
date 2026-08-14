package winservices

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbe(t *testing.T, config map[string]interface{}) *WinServicesProbe {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	p, err := NewWinServicesProbe(config, baseLogger)
	if err != nil {
		t.Fatalf("NewWinServicesProbe: %v", err)
	}
	return p.(*WinServicesProbe)
}

func findPoint(points []data_store.DataPoint, name, svc string) (data_store.DataPoint, bool) {
	for _, p := range points {
		if p.Name != name {
			continue
		}
		if svc == "" {
			return p, true
		}
		for _, tag := range p.Tags {
			if tag.Key == "windows.service.name" && tag.Value == svc {
				return p, true
			}
		}
	}
	return data_store.DataPoint{}, false
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Interval != DefaultInterval {
		t.Errorf("Interval = %s, want %s", cfg.Interval, DefaultInterval)
	}
	if len(cfg.Services) != 0 {
		t.Errorf("Services = %v, want empty", cfg.Services)
	}
}

func TestParseConfig_ServicesAndInterval(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"services": []interface{}{"Spooler", "", "wuauserv"},
		"interval": 45,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if want := []string{"Spooler", "wuauserv"}; len(cfg.Services) != 2 || cfg.Services[0] != want[0] || cfg.Services[1] != want[1] {
		t.Errorf("Services = %v, want %v", cfg.Services, want)
	}
	if cfg.Interval != 45*time.Second {
		t.Errorf("Interval = %s, want 45s", cfg.Interval)
	}
}

func TestParseConfig_DurationString(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"interval": "2m"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Interval != 2*time.Minute {
		t.Errorf("Interval = %s, want 2m", cfg.Interval)
	}
}

func TestParseConfig_BadInterval(t *testing.T) {
	if _, err := parseConfig(map[string]interface{}{"interval": "nope"}); err == nil {
		t.Fatal("expected error for invalid interval")
	}
}

func TestCollect_EmitsUpAndPerServiceMetrics(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{
			{name: "Spooler", state: stateRunning},
			{name: "wuauserv", state: stateStopped},
		}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up, ok := findPoint(points, "senhub.winservices.up", "")
	if !ok {
		t.Fatal("missing senhub.winservices.up")
	}
	if up.Value != 1 {
		t.Errorf("up = %v, want 1", up.Value)
	}

	state, ok := findPoint(points, "windows.service.state", "Spooler")
	if !ok {
		t.Fatal("missing windows.service.state for Spooler")
	}
	if state.Value != 1 {
		t.Errorf("Spooler state = %v, want 1 (running)", state.Value)
	}

	status, ok := findPoint(points, "windows.service.status", "Spooler")
	if !ok {
		t.Fatal("missing windows.service.status for Spooler")
	}
	if status.Value != float64(stateRunning) {
		t.Errorf("Spooler status = %v, want %d", status.Value, stateRunning)
	}

	stoppedState, _ := findPoint(points, "windows.service.state", "wuauserv")
	if stoppedState.Value != 0 {
		t.Errorf("wuauserv state = %v, want 0 (not running)", stoppedState.Value)
	}
	stoppedStatus, _ := findPoint(points, "windows.service.status", "wuauserv")
	if stoppedStatus.Value != float64(stateStopped) {
		t.Errorf("wuauserv status = %v, want %d", stoppedStatus.Value, stateStopped)
	}
}

func TestCollect_FailureEmitsUpZero(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.collect = func(_ []string) ([]serviceState, error) {
		return nil, context.DeadlineExceeded
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return an error on SCM failure: %v", err)
	}

	up, ok := findPoint(points, "senhub.winservices.up", "")
	if !ok {
		t.Fatal("missing senhub.winservices.up")
	}
	if up.Value != 0 {
		t.Errorf("up = %v, want 0 on collection failure", up.Value)
	}
}

func TestCollect_EnrichesProbeName(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.SetName("Windows Services")
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{{name: "Spooler", state: stateRunning}}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, pt := range points {
		var hasProbeName, hasProbeType bool
		for _, tag := range pt.Tags {
			if tag.Key == "probe_name" && tag.Value == "Windows Services" {
				hasProbeName = true
			}
			if tag.Key == "probe_type" && tag.Value == ProbeType {
				hasProbeType = true
			}
		}
		if !hasProbeName || !hasProbeType {
			t.Errorf("datapoint %q missing probe_name/probe_type tags: %+v", pt.Name, pt.Tags)
		}
	}
}

func TestEntitySource_EmitsNothingWithoutAHostID(t *testing.T) {
	s := newEntitySource()
	obs, ok := s.Observe()
	// Nothing at all without a host id. The identity is built from it since
	// #742, and the previous behaviour — emitting a constant id with no
	// relation — is exactly what collapsed every Windows host onto one node.
	if ok {
		t.Errorf("Observe returned an entity with no host id: %+v", obs)
	}
}

func TestEntitySource_RunsOnRelation(t *testing.T) {
	s := newEntitySource()
	s.setHostID("test-host-uuid")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok = false, want true")
	}
	// runs_on plus the one-off same_as that retires the collapsed identity.
	var r entity.Relation
	for _, rel := range obs.Relations {
		if rel.Type == relRunsOn {
			r = rel
		}
	}
	if r.Type != relRunsOn {
		t.Fatalf("no runs_on among %d relations", len(obs.Relations))
	}
	if r.Type != relRunsOn {
		t.Errorf("Relation.Type = %q, want %q", r.Type, relRunsOn)
	}
	if r.FromType != entityTypeServiceInstance {
		t.Errorf("FromType = %q, want %q", r.FromType, entityTypeServiceInstance)
	}
	if r.ToType != entityTypeHost {
		t.Errorf("ToType = %q, want %q", r.ToType, entityTypeHost)
	}
	if r.ToID[idKeyHost] != "test-host-uuid" {
		t.Errorf("ToID[host.id] = %v, want %q", r.ToID[idKeyHost], "test-host-uuid")
	}
	// FromID must match the service.instance entity's ID exactly, and that id
	// must carry the host — a constant here is what #742 fixed.
	want := "winservices@test-host-uuid"
	if r.FromID[idKeyServiceInstanceID] != want {
		t.Errorf("FromID[service.instance.id] = %v, want %q", r.FromID[idKeyServiceInstanceID], want)
	}
	if obs.Entities[0].ID[idKeyServiceInstanceID] != want {
		t.Errorf("entity id = %v, want %q", obs.Entities[0].ID[idKeyServiceInstanceID], want)
	}
}

// Two hosts must not share one node. That is the whole defect: the id carried
// no host component, so every Windows machine in a fleet collapsed onto a
// single service.instance which then fanned out to all of them.
func TestEntitySource_IdentityIsUniquePerHost(t *testing.T) {
	idFor := func(host string) any {
		s := newEntitySource()
		s.setHostID(host)
		obs, ok := s.Observe()
		if !ok {
			t.Fatalf("Observe ok=false for host %q", host)
		}
		return obs.Entities[0].ID[idKeyServiceInstanceID]
	}
	if a, b := idFor("host-a"), idFor("host-b"); a == b {
		t.Fatalf("two hosts produced the same identity %v", a)
	}
}

// The retired identity is announced explicitly rather than left to expire, so
// the consumer reads a decision instead of a producer going silent.
func TestEntitySource_AnnouncesTheRetiredIdentity(t *testing.T) {
	s := newEntitySource()
	s.setHostID("host-a")
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	var alias *entity.Relation
	for i := range obs.Relations {
		if obs.Relations[i].Type == entity.RelSameAs {
			alias = &obs.Relations[i]
		}
	}
	if alias == nil {
		t.Fatal("no same_as edge to the retired identity")
	}
	if alias.ToID[idKeyServiceInstanceID] != legacyServiceInstanceID {
		t.Errorf("alias points at %v, want %q", alias.ToID[idKeyServiceInstanceID], legacyServiceInstanceID)
	}
	if alias.Attributes["confidence"] != 1.0 {
		t.Errorf("confidence = %v; without it the consumer treats the alias as inert", alias.Attributes["confidence"])
	}
}

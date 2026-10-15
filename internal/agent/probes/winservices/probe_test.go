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

func boolPtr(b bool) *bool { return &b }

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
	if cfg.Detail != serviceDetailState {
		t.Errorf("Detail = %q, want %q", cfg.Detail, serviceDetailState)
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

func TestParseConfig_Detail(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"detail": "full"})
	if err != nil {
		t.Fatalf("parseConfig with detail=full: %v", err)
	}
	if cfg.Detail != serviceDetailFull {
		t.Errorf("Detail = %q, want %q", cfg.Detail, serviceDetailFull)
	}

	if _, err := parseConfig(map[string]interface{}{"detail": "verbose"}); err == nil {
		t.Error("expected error for invalid detail")
	}
}

func TestSelectAutomaticServices(t *testing.T) {
	states := []serviceState{
		{name: "auto", state: stateStopped, startType: startAutomatic},
		{name: "delayed", state: stateStopped, startType: startAutomaticDelayed},
		{name: "manual", state: stateStopped, startType: startManual},
		{name: "disabled", state: stateStopped, startType: startDisabled},
		{name: "boot", state: stateStopped, startType: startBoot},
		{name: "system", state: stateStopped, startType: startSystem},
		{name: "unknown", state: stateStopped, startType: -1},
	}
	got := selectAutomaticServices(states)
	if len(got) != 2 {
		t.Fatalf("selectAutomaticServices: got %d services; want 2", len(got))
	}
	if got[0].name != "auto" || got[1].name != "delayed" {
		t.Errorf("selectAutomaticServices = %v; want auto and delayed", got)
	}
}

func TestCollect_EmitsUpAndPerServiceMetrics(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"detail": "full"})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{
			{name: "Spooler", state: stateRunning, startType: startAutomatic},
			{name: "wuauserv", state: stateStopped, startType: startAutomatic},
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

func TestCollect_DefaultSelectionAndDetailState(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{
			{name: "automatic", state: stateRunning, startType: startAutomatic},
			{name: "delayed", state: stateStopped, startType: startAutomaticDelayed},
			{name: "manual", state: stateRunning, startType: startManual},
			{name: "disabled", state: stateStopped, startType: startDisabled},
			{name: "unknown", state: stateRunning, startType: -1},
		}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if _, ok := findPoint(points, "senhub.winservices.up", ""); !ok {
		t.Error("missing senhub.winservices.up")
	}
	if _, ok := findPoint(points, "windows.service.state", "automatic"); !ok {
		t.Error("missing state for automatic service")
	}
	if _, ok := findPoint(points, "windows.service.state", "delayed"); !ok {
		t.Error("missing state for delayed automatic service")
	}
	if _, ok := findPoint(points, "windows.service.state", "manual"); ok {
		t.Error("default selection must not emit a manual service")
	}
	if _, ok := findPoint(points, "windows.service.state", "disabled"); ok {
		t.Error("default selection must not emit a disabled service")
	}
	if _, ok := findPoint(points, "windows.service.state", "unknown"); ok {
		t.Error("default selection must not emit a service with unknown start type")
	}
	if _, ok := findPoint(points, "windows.service.status", "automatic"); ok {
		t.Error("detail=state must not emit windows.service.status")
	}
	if _, ok := findPoint(points, "windows.service.start_type", "automatic"); ok {
		t.Error("detail=state must not emit windows.service.start_type")
	}
}

func TestCollect_ExplicitSelectionIncludesManual(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"services": []interface{}{"manual"}})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{
			{name: "manual", state: stateRunning, startType: startManual},
		}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if _, ok := findPoint(points, "windows.service.state", "manual"); !ok {
		t.Error("explicit service list must keep a manual service")
	}
	if _, ok := findPoint(points, "windows.service.status", "manual"); ok {
		t.Error("detail=state must not emit windows.service.status")
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
		return []serviceState{{name: "Spooler", state: stateRunning, startType: startAutomatic}}, nil
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
	s.setServices("", "host-name", []serviceState{
		{name: "Spooler", state: stateRunning, startType: startAutomatic},
	})
	obs, ok := s.Observe()
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
	var r entity.Relation
	for _, rel := range obs.Relations {
		if rel.Type == relRunsOn && rel.FromType == entityTypeServiceInstance {
			r = rel
		}
	}
	if r.Type != relRunsOn {
		t.Fatalf("no service.instance runs_on among %d relations", len(obs.Relations))
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
	want := "winservices@test-host-uuid"
	if r.FromID[idKeyServiceInstanceID] != want {
		t.Errorf("FromID[service.instance.id] = %v, want %q", r.FromID[idKeyServiceInstanceID], want)
	}
	if obs.Entities[0].ID[idKeyServiceInstanceID] != want {
		t.Errorf("entity id = %v, want %q", obs.Entities[0].ID[idKeyServiceInstanceID], want)
	}
}

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

func TestEntitySource_EmitsHostServicePerSelectedService(t *testing.T) {
	s := newEntitySource()
	s.setServices("host-a", "host-name", []serviceState{
		{name: "Spooler", state: stateRunning, startType: startAutomaticDelayed, startDelayed: true, triggerStart: boolPtr(true)},
		{name: "wuauserv", state: stateStopped, startType: startManual, startDelayed: false, triggerStart: boolPtr(false)},
	})

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}

	var serviceInstanceCount, hostServiceCount int
	for _, e := range obs.Entities {
		switch e.Type {
		case entityTypeServiceInstance:
			serviceInstanceCount++
		case entity.TypeHostService:
			hostServiceCount++
		}
	}
	if serviceInstanceCount != 1 {
		t.Fatalf("service.instance entities = %d, want 1", serviceInstanceCount)
	}
	if hostServiceCount != 2 {
		t.Fatalf("host.service entities = %d, want 2", hostServiceCount)
	}

	var spooler, wuau *entity.Entity
	for i := range obs.Entities {
		if obs.Entities[i].Type != entity.TypeHostService {
			continue
		}
		switch obs.Entities[i].ID["host.service.name"] {
		case "Spooler":
			spooler = &obs.Entities[i]
		case "wuauserv":
			wuau = &obs.Entities[i]
		}
	}
	if spooler == nil || wuau == nil {
		t.Fatalf("missing host.service entities: %+v", obs.Entities)
	}

	if got := spooler.Attributes["host.service.start_mode"]; got != "auto_start" {
		t.Errorf("Spooler start_mode = %v, want auto_start", got)
	}
	if got := spooler.Attributes["host.service.start_delayed"]; got != true {
		t.Errorf("Spooler start_delayed = %v, want true", got)
	}
	if got := spooler.Attributes["host.service.trigger_start"]; got != true {
		t.Errorf("Spooler trigger_start = %v, want true", got)
	}
	if got := spooler.Attributes["host.service.state"]; got != "running" {
		t.Errorf("Spooler state = %v, want running", got)
	}

	if got := wuau.Attributes["host.service.start_mode"]; got != "demand_start" {
		t.Errorf("wuauserv start_mode = %v, want demand_start", got)
	}
	if got := wuau.Attributes["host.service.start_delayed"]; got != false {
		t.Errorf("wuauserv start_delayed = %v, want false", got)
	}
	if got := wuau.Attributes["host.service.trigger_start"]; got != false {
		t.Errorf("wuauserv trigger_start = %v, want false", got)
	}
	if got := wuau.Attributes["host.service.state"]; got != "stopped" {
		t.Errorf("wuauserv state = %v, want stopped", got)
	}

	var hostServiceRuns int
	for _, rel := range obs.Relations {
		if rel.Type == relRunsOn && rel.FromType == entity.TypeHostService {
			hostServiceRuns++
			if rel.ToType != entityTypeHost || rel.ToID[idKeyHost] != "host-a" {
				t.Errorf("host.service runs_on must target host-a: %+v", rel)
			}
		}
	}
	if hostServiceRuns != 2 {
		t.Errorf("host.service runs_on relations = %d, want 2", hostServiceRuns)
	}

	foundProbeAlias := false
	for _, rel := range obs.Relations {
		if rel.Type == entity.RelSameAs {
			foundProbeAlias = true
		}
	}
	if !foundProbeAlias {
		t.Error("missing probe-level same_as relation")
	}
}

func TestEntitySource_NoHostServiceWithoutHostID(t *testing.T) {
	s := newEntitySource()
	s.setServices("", "host-name", []serviceState{
		{name: "Spooler", state: stateRunning, startType: startAutomatic},
	})
	if obs, ok := s.Observe(); ok {
		t.Errorf("Observe returned entities without host id: %+v", obs)
	}
}

func TestCollect_ReportsASelectedServiceItCannotFind(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"services": []interface{}{"spooler", "Spooler, W32Time"}})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{{name: "Spooler", state: stateRunning, startType: startAutomatic}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := p.Collect(); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	if !p.reportedMissing["Spooler, W32Time"] {
		t.Error("the unmatched selection was not reported")
	}
	if p.reportedMissing["spooler"] {
		t.Error("a service found under another case was reported missing")
	}
	if len(p.reportedMissing) != 1 {
		t.Errorf("reportedMissing = %v, want one entry", p.reportedMissing)
	}
}

func TestCollect_EmitsTheStartTypeWhenItIsKnown(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"services": []interface{}{"Spooler", "locked"},
		"detail":   "full",
	})
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{
			{name: "Spooler", state: stateStopped, startType: startAutomaticDelayed},
			{name: "locked", state: stateRunning, startType: -1},
		}, nil
	}
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	st, ok := findPoint(points, "windows.service.start_type", "Spooler")
	if !ok || st.Value != float64(startAutomaticDelayed) {
		t.Errorf("Spooler start type = %v (found %v), want %d", st.Value, ok, startAutomaticDelayed)
	}
	if _, ok := findPoint(points, "windows.service.start_type", "locked"); ok {
		t.Error("a start type was emitted for a service whose configuration could not be read")
	}
	if _, ok := findPoint(points, "windows.service.state", "locked"); !ok {
		t.Error("the state of that service must still be reported")
	}
}

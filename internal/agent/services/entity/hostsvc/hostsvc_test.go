package hostsvc

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildObservation_Listeners(t *testing.T) {
	ls := []listener{
		{Pid: 1001, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},
		{Pid: 22, Proc: "sshd", Address: "0.0.0.0", Port: 22, Transport: "tcp"},
	}
	obs := buildObservation("h-1", ls)

	if len(obs.Entities) != 2 || len(obs.Relations) != 2 {
		t.Fatalf("obs = %+v", obs)
	}

	nginx, found := entityByProc(obs, "nginx")
	if !found {
		t.Fatalf("nginx listener not emitted: %+v", obs.Entities)
	}
	if nginx.Type != entityTypeServiceListener {
		t.Errorf("type = %q, want service.listener", nginx.Type)
	}
	if nginx.ID[idKeyServiceEndpoint] != "h-1:80/tcp" {
		t.Errorf("endpoint id = %v, want h-1:80/tcp", nginx.ID[idKeyServiceEndpoint])
	}
	if nginx.Attributes[attrProcessPID] != int64(1001) ||
		nginx.Attributes[attrTransport] != "tcp" ||
		nginx.Attributes[attrListenAddress] != "0.0.0.0" {
		t.Errorf("attrs = %+v", nginx.Attributes)
	}

	for _, r := range obs.Relations {
		if r.Type != relRunsOn {
			t.Errorf("relation type = %q, want runs_on", r.Type)
		}
		if r.FromType != entityTypeServiceListener || r.ToType != entityTypeHost || r.ToID[idKeyHost] != "h-1" {
			t.Errorf("runs_on wrong: %+v", r)
		}
	}
}

func TestBuildObservation_EmptyGuards(t *testing.T) {
	if o := buildObservation("", []listener{{Port: 80, Transport: "tcp"}}); len(o.Entities) != 0 {
		t.Error("no hostID → empty")
	}
	if o := buildObservation("h", nil); len(o.Entities) != 0 {
		t.Error("no listeners → empty")
	}
}

func TestBuildObservation_ProcAndPidOmittedWhenAbsent(t *testing.T) {
	obs := buildObservation("h-1", []listener{{Port: 443, Transport: "tcp"}}) // no Proc, Pid 0, no Address
	a := obs.Entities[0].Attributes
	if _, ok := a[attrProcessName]; ok {
		t.Error("process.executable.name should be omitted when unknown")
	}
	if _, ok := a[attrProcessPID]; ok {
		t.Error("process.pid should be omitted when 0")
	}
	if a[attrTransport] != "tcp" {
		t.Errorf("transport always present: %+v", a)
	}
}

func TestObserve_CachesBetweenRefreshes(t *testing.T) {
	calls := 0
	s := &Source{
		hostID:    func() string { return "h-1" },
		enumerate: func() []listener { calls++; return []listener{{Pid: 1, Proc: "x", Port: 80, Transport: "tcp"}} },
		refresh:   time.Hour,
	}
	o1 := s.Observe()
	o2 := s.Observe() // within the refresh window → served from cache, no re-enumeration
	if calls != 1 {
		t.Errorf("enumerate called %d times, want 1 (cached within refresh)", calls)
	}
	if len(o1.Entities) != 1 || len(o2.Entities) != 1 {
		t.Errorf("obs1=%d obs2=%d entities", len(o1.Entities), len(o2.Entities))
	}
}

func entityByProc(obs entity.Observation, proc string) (entity.Entity, bool) {
	for _, e := range obs.Entities {
		if e.Attributes[attrProcessName] == proc {
			return e, true
		}
	}
	return entity.Entity{}, false
}

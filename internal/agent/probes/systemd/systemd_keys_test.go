//go:build linux

package systemd

import (
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(ts []tags.Tag, key string) (string, bool) {
	for _, t := range ts {
		if t.Key == key {
			return t.Value, true
		}
	}
	return "", false
}

// TestEntitySourceNoHostIDNoKey checks a unit gets no identity while the host
// id is unknown: the entity is not emitted then, and a key would name nothing.
func TestEntitySourceNoHostIDNoKey(t *testing.T) {
	s := newEntitySource("some-host")
	s.setUnits([]hostServiceUnit{
		{Name: "nginx.service", ActiveState: "active", UnitFileState: "enabled"},
	}, "", "some-host")

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe() ok=false")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Fatalf("no host id must yield no entity, got %+v", obs)
	}

	s.setUnits([]hostServiceUnit{
		{Name: "nginx.service", ActiveState: "active", UnitFileState: "enabled"},
	}, "host-123", "some-host")

	obs, ok = s.Observe()
	if !ok || len(obs.Entities) != 1 {
		t.Fatalf("Observe: ok=%v, %d entities; want 1", ok, len(obs.Entities))
	}
	if got := obs.Entities[0].ID["host.id"]; got != "host-123" {
		t.Fatalf("entity host.id = %v; want host-123", got)
	}
	if got := obs.Entities[0].ID["host.service.name"]; got != "nginx.service" {
		t.Fatalf("entity host.service.name = %v; want nginx.service", got)
	}
}

func TestBuildDatapointsCarriesHostServiceTags(t *testing.T) {
	u := makeUnit("nginx.service", "active", "running", "loaded")

	pts := buildDatapoints(u, time.Now(), nil, "host-123", "some-host", systemdDetailFull)
	if len(pts) == 0 {
		t.Fatal("buildDatapoints returned no datapoints")
	}

	for _, p := range pts {
		if got, ok := tagValue(p.Tags, "host.id"); !ok || got != "host-123" {
			t.Fatalf("datapoint %q missing host.id = %q: tags=%v", p.Name, got, p.Tags)
		}
		if got, ok := tagValue(p.Tags, "host.service.name"); !ok || got != "nginx.service" {
			t.Fatalf("datapoint %q missing host.service.name = %q: tags=%v", p.Name, got, p.Tags)
		}
		if got, ok := tagValue(p.Tags, "host.name"); !ok || got != "some-host" {
			t.Fatalf("datapoint %q missing host.name = %q: tags=%v", p.Name, got, p.Tags)
		}
		if _, ok := tagValue(p.Tags, "service.instance.id"); ok {
			t.Fatalf("datapoint %q must not carry service.instance.id: tags=%v", p.Name, p.Tags)
		}
	}
}

func TestBuildDatapointsOmitsIdentityWithoutHostID(t *testing.T) {
	u := makeUnit("nginx.service", "active", "running", "loaded")

	pts := buildDatapoints(u, time.Now(), nil, "", "some-host", systemdDetailState)
	if len(pts) == 0 {
		t.Fatal("buildDatapoints returned no datapoints")
	}

	for _, p := range pts {
		if _, ok := tagValue(p.Tags, "host.id"); ok {
			t.Fatalf("datapoint %q must not carry host.id without hostID: tags=%v", p.Name, p.Tags)
		}
		if _, ok := tagValue(p.Tags, "host.service.name"); ok {
			t.Fatalf("datapoint %q must not carry host.service.name without hostID: tags=%v", p.Name, p.Tags)
		}
		if got, ok := tagValue(p.Tags, "host.name"); !ok || got != "some-host" {
			t.Fatalf("datapoint %q missing host.name = %q: tags=%v", p.Name, got, p.Tags)
		}
	}
}

func TestBuildCheckResultDatapointOK(t *testing.T) {
	baseTags := baseTagsForUnit(makeUnit("nginx.service", "active", "running", "loaded"), "host-123", "some-host")

	dp := buildCheckResultDatapoint(time.Now(), nil, baseTags)

	if dp.Name != dbcommon.CheckResultMetric {
		t.Fatalf("check result name = %q; want %q", dp.Name, dbcommon.CheckResultMetric)
	}
	if dp.Value != float64(dbcommon.CheckOK) {
		t.Fatalf("check result value = %v; want 0", dp.Value)
	}
	if _, ok := tagValue(dp.Tags, "service.instance.id"); ok {
		t.Fatalf("check result must not carry service.instance.id: tags=%v", dp.Tags)
	}
	if got, ok := tagValue(dp.Tags, "host.id"); !ok || got != "host-123" {
		t.Fatalf("check result missing host.id = %q: tags=%v", got, dp.Tags)
	}
	gotType, ok := tagValue(dp.Tags, "metric_type")
	if !ok || gotType != "status" {
		t.Fatalf("check result metric_type = %q; want %q", gotType, "status")
	}
}

func TestBuildCheckResultDatapointFailedConnection(t *testing.T) {
	err := &net.DNSError{IsNotFound: true}
	baseTags := baseTagsForUnit(makeUnit("nginx.service", "active", "running", "loaded"), "host-123", "some-host")

	dp := buildCheckResultDatapoint(time.Now(), err, baseTags)

	wantClass := dbcommon.ClassifyError(err)
	if wantClass != dbcommon.CheckTargetDown {
		t.Fatalf("ClassifyError = %v; want CheckTargetDown", wantClass)
	}
	if dp.Value != float64(wantClass) {
		t.Fatalf("check result value = %v; want %v", dp.Value, float64(wantClass))
	}
	if _, ok := tagValue(dp.Tags, "service.instance.id"); ok {
		t.Fatalf("check result must not carry service.instance.id: tags=%v", dp.Tags)
	}
	if got, ok := tagValue(dp.Tags, "host.id"); !ok || got != "host-123" {
		t.Fatalf("check result missing host.id = %q: tags=%v", got, dp.Tags)
	}
}

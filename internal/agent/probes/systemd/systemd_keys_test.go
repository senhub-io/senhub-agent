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

func TestEntitySourceFallbackInstanceID(t *testing.T) {
	s := newEntitySource("fallback-host")
	s.setUnits([]string{"nginx.service"}, "", "fallback-host")

	const wantID = "systemd://fallback-host/nginx.service"
	if got := s.ServiceInstanceID("nginx.service"); got != wantID {
		t.Fatalf("ServiceInstanceID = %q; want %q", got, wantID)
	}

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after setUnits")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe entities = %d; want 1", len(obs.Entities))
	}
	if got := obs.Entities[0].ID["service.instance.id"]; got != wantID {
		t.Fatalf("entity service.instance.id = %v; want %q", got, wantID)
	}
}

func TestBuildDatapointsCarriesServiceInstanceID(t *testing.T) {
	u := makeUnit("nginx.service", "active", "running", "loaded")
	const entityID = "systemd://host-123/nginx.service"

	pts := buildDatapoints(u, time.Now(), nil, entityID)
	if len(pts) == 0 {
		t.Fatal("buildDatapoints returned no datapoints")
	}

	for _, p := range pts {
		got, ok := tagValue(p.Tags, "service.instance.id")
		if !ok || got != entityID {
			t.Fatalf("datapoint %q missing service.instance.id = %q: tags=%v", p.Name, entityID, p.Tags)
		}
	}
}

func TestBuildCheckResultDatapointOK(t *testing.T) {
	const entityID = "systemd://host-123/nginx.service"
	baseTags := baseTagsForUnit(makeUnit("nginx.service", "active", "running", "loaded"), entityID)

	dp := buildCheckResultDatapoint(time.Now(), nil, baseTags)

	if dp.Name != dbcommon.CheckResultMetric {
		t.Fatalf("check result name = %q; want %q", dp.Name, dbcommon.CheckResultMetric)
	}
	if dp.Value != float64(dbcommon.CheckOK) {
		t.Fatalf("check result value = %v; want 0", dp.Value)
	}
	gotID, ok := tagValue(dp.Tags, "service.instance.id")
	if !ok || gotID != entityID {
		t.Fatalf("check result missing service.instance.id = %q: tags=%v", entityID, dp.Tags)
	}
	gotType, ok := tagValue(dp.Tags, "metric_type")
	if !ok || gotType != "status" {
		t.Fatalf("check result metric_type = %q; want %q", gotType, "status")
	}
}

func TestBuildCheckResultDatapointFailedConnection(t *testing.T) {
	const entityID = "systemd://fallback-host/nginx.service"
	err := &net.DNSError{IsNotFound: true}
	baseTags := baseTagsForUnit(makeUnit("nginx.service", "active", "running", "loaded"), entityID)

	dp := buildCheckResultDatapoint(time.Now(), err, baseTags)

	wantClass := dbcommon.ClassifyError(err)
	if wantClass != dbcommon.CheckTargetDown {
		t.Fatalf("ClassifyError = %v; want CheckTargetDown", wantClass)
	}
	if dp.Value != float64(wantClass) {
		t.Fatalf("check result value = %v; want %v", dp.Value, float64(wantClass))
	}
	gotID, ok := tagValue(dp.Tags, "service.instance.id")
	if !ok || gotID != entityID {
		t.Fatalf("check result missing service.instance.id = %q: tags=%v", entityID, dp.Tags)
	}
}

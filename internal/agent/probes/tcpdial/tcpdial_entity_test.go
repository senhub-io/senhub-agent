package tcpdial

import (
	"context"
	"fmt"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

func newEntityTestProbe(t *testing.T, agentID string, targets ...string) *TCPDialProbe {
	t.Helper()
	p := newTestProbe(t, targets...)
	p.hostID = "host-1"
	p.entitySrc.getAgentID = func() string { return agentID }
	p.dial = func(_ context.Context, target string) dialResult {
		return dialResult{target: target, up: true, duration: time.Millisecond}
	}
	return p
}

func tagValue(ts []tags.Tag, key string) (string, bool) {
	for _, tg := range ts {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func findDatapoint(t *testing.T, points []data_store.DataPoint, name, target string) data_store.DataPoint {
	t.Helper()
	for _, dp := range points {
		if dp.Name != name {
			continue
		}
		if got, ok := tagValue(dp.Tags, "target"); ok && got == target {
			return dp
		}
	}
	t.Fatalf("no datapoint %s for target %s", name, target)
	return data_store.DataPoint{}
}

func TestEntitySourceLocalTarget(t *testing.T) {
	p := newEntityTestProbe(t, "agent-1", "localhost:443")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe not ready after Collect")
	}
	if len(obs.Entities) != 0 {
		t.Fatalf("listener entity emitted: %#v", obs.Entities)
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("relations = %d, want 1", len(obs.Relations))
	}

	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" || rel.FromID["service.instance.id"] != "agent-1" {
		t.Errorf("relation from = %s %#v, want agent-1", rel.FromType, rel.FromID)
	}
	if rel.ToType != "service.listener" {
		t.Errorf("relation target type = %q, want service.listener", rel.ToType)
	}

	wantID := entity.ListenerEndpoint("host-1", 443, "tcp")
	if got := rel.ToID["service.endpoint"]; got != wantID {
		t.Errorf("relation target id = %v, want %q", got, wantID)
	}

	up := findDatapoint(t, points, "senhub.tcpdial.up", "localhost:443")
	if got, ok := tagValue(up.Tags, "service.endpoint"); !ok || got != wantID {
		t.Errorf("series service.endpoint = %q, ok=%v; want %q", got, ok, wantID)
	}
}

func TestEntitySourceRemoteIP(t *testing.T) {
	p := newEntityTestProbe(t, "agent-1", "8.8.8.8:443")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe not ready after Collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(obs.Entities))
	}

	ent := obs.Entities[0]
	if ent.Type != "network.endpoint" {
		t.Errorf("entity type = %q, want network.endpoint", ent.Type)
	}
	if got := ent.ID["server.address"]; got != "8.8.8.8" {
		t.Errorf("server.address = %v, want 8.8.8.8", got)
	}
	if got := ent.ID["server.port"]; got != "443" {
		t.Errorf("server.port = %v, want 443", got)
	}
	if got := ent.ID["network.transport"]; got != "tcp" {
		t.Errorf("network.transport = %v, want tcp", got)
	}
	if _, exists := ent.ID["host.id"]; exists {
		t.Errorf("remote endpoint must not carry host.id: %#v", ent.ID)
	}

	if len(obs.Relations) != 1 {
		t.Fatalf("relations = %d, want 1", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.ToType != "network.endpoint" {
		t.Errorf("relation target type = %q, want network.endpoint", rel.ToType)
	}

	up := findDatapoint(t, points, "senhub.tcpdial.up", "8.8.8.8:443")
	if got, ok := tagValue(up.Tags, "server.address"); !ok || got != "8.8.8.8" {
		t.Errorf("series server.address = %q, ok=%v; want 8.8.8.8", got, ok)
	}
	if got, ok := tagValue(up.Tags, "server.port"); !ok || got != "443" {
		t.Errorf("series server.port = %q, ok=%v; want 443", got, ok)
	}
	if got, ok := tagValue(up.Tags, "network.transport"); !ok || got != "tcp" {
		t.Errorf("series network.transport = %q, ok=%v; want tcp", got, ok)
	}
}

func TestEntitySourceRemoteDNSNameLowercased(t *testing.T) {
	p := newEntityTestProbe(t, "agent-1", "DB-Server.Example.COM:389")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe not ready after Collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(obs.Entities))
	}

	ent := obs.Entities[0]
	if got := ent.ID["server.address"]; got != "db-server.example.com" {
		t.Errorf("server.address = %v, want db-server.example.com", got)
	}

	up := findDatapoint(t, points, "senhub.tcpdial.up", "DB-Server.Example.COM:389")
	if got, ok := tagValue(up.Tags, "server.address"); !ok || got != "db-server.example.com" {
		t.Errorf("series server.address = %q, ok=%v; want db-server.example.com", got, ok)
	}
}

func TestEntitySourceAgentIDEmptySkipsRelations(t *testing.T) {
	p := newEntityTestProbe(t, "", "8.8.8.8:53")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe not ready after Collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(obs.Entities))
	}
	if len(obs.Relations) != 0 {
		t.Errorf("relations = %d, want 0 with empty agentID", len(obs.Relations))
	}

	up := findDatapoint(t, points, "senhub.tcpdial.up", "8.8.8.8:53")
	if got, ok := tagValue(up.Tags, "server.address"); !ok || got != "8.8.8.8" {
		t.Errorf("series server.address = %q, ok=%v; want 8.8.8.8", got, ok)
	}
}

func TestCheckResultRefusedConnection(t *testing.T) {
	p := newEntityTestProbe(t, "agent-1", "8.8.8.8:443")
	p.dial = func(_ context.Context, target string) dialResult {
		return dialResult{target: target, err: fmt.Errorf("connection refused")}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	status := findDatapoint(t, points, dbcommon.CheckResultMetric, "8.8.8.8:443")
	if status.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result for refused connection = %v, want %d", status.Value, dbcommon.CheckTargetDown)
	}

	up := findDatapoint(t, points, "senhub.tcpdial.up", "8.8.8.8:443")
	if up.Value != 0 {
		t.Errorf("tcpdial.up for refused connection = %v, want 0", up.Value)
	}
}

func TestHTTPStatusClassificationCheckResult(t *testing.T) {
	if got := dbcommon.ClassifyHTTPStatus(401); got != dbcommon.CheckAuthDenied {
		t.Fatalf("ClassifyHTTPStatus(401) = %v, want %v", got, dbcommon.CheckAuthDenied)
	}
}

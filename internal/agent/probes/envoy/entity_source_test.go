package envoy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// testEntitySource builds an entity source with injected dependencies so
// no live Envoy endpoint or host gopsutil call is needed.
func testEntitySource(
	instanceName string,
	nodeIDFunc func() string,
	hostIDFunc func() string,
) *envoyEntitySource {
	s := &envoyEntitySource{
		descAttr: map[string]any{
			"service.name":   "envoy",
			"server.address": "127.0.0.1",
			"server.port":    int64(9901),
		},
		serverAddr: "127.0.0.1",
		fetchInfo:  func() (string, string) { return nodeIDFunc(), "" },
		hostID:     hostIDFunc,
	}
	if instanceName != "" {
		s.idOnce.Do(func() {
			s.pinnedID = instanceName
			s.idPinned = true
		})
	}
	return s
}

// TestEntitySource_InstanceNameOverride verifies that operator-supplied
// instance_name is used verbatim and pinned immediately (precedence 1).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	fetchCalled := false
	s := testEntitySource(
		"my-envoy-prod",
		func() string { fetchCalled = true; return "node-from-envoy" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; want true with instance_name override")
	}
	if fetchCalled {
		t.Error("fetchNodeID must not be called when instance_name is set (precedence 1)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "my-envoy-prod" {
		t.Errorf("service.instance.id = %q, want %q", gotID, "my-envoy-prod")
	}
}

// TestEntitySource_TechIDPinned verifies that a non-empty node.id from
// /server_info is used as "envoy:<id>" (precedence 2).
func TestEntitySource_TechIDPinned(t *testing.T) {
	s := testEntitySource(
		"",
		func() string { return "envoy-node-abc123" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; want true after tech-id is available")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	want := "envoy:envoy-node-abc123"
	if gotID != want {
		t.Errorf("service.instance.id = %q, want %q", gotID, want)
	}
}

// TestEntitySource_NotEmittedBeforeFirstUp verifies that Observe returns
// ok=false (entity not emitted) when the probe has not yet reported up=true,
// regardless of whether a node id would be available.
func TestEntitySource_NotEmittedBeforeFirstUp(t *testing.T) {
	s := testEntitySource(
		"",
		func() string { return "envoy-node-xyz" },
		func() string { return "host-uuid" },
	)
	// up is false by default — never called setReachable(true).

	_, ok := s.Observe()
	if ok {
		t.Error("Observe returned ok=true before the first successful collect; want false")
	}
	if s.idPinned {
		t.Error("idPinned must be false before any successful collect cycle")
	}
}

// TestEntitySource_FallbackToHostID verifies that when node.id is empty the
// entity source falls back to "envoy@<host.id>" (precedence 3).
func TestEntitySource_FallbackToHostID(t *testing.T) {
	s := testEntitySource(
		"",
		func() string { return "" }, // node.id empty
		func() string { return "6ba7b810-9dad-11d1-80b4-00c04fd430c8" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; want true after fallback is pinned")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	want := "envoy@6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	if gotID != want {
		t.Errorf("service.instance.id = %q, want %q", gotID, want)
	}
}

// TestEntitySource_FallbackLastResort verifies that when both node.id and
// host.id are unavailable the id degrades to the bare "envoy" string.
func TestEntitySource_FallbackLastResort(t *testing.T) {
	s := testEntitySource(
		"",
		func() string { return "" },
		func() string { return "" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; want true even with last-resort id")
	}
	gotID := obs.Entities[0].ID["service.instance.id"]
	if gotID != "envoy" {
		t.Errorf("service.instance.id = %q, want %q", gotID, "envoy")
	}
}

// TestEntitySource_IDPinnedOnce verifies that calling Observe multiple times
// after the first successful collect does not re-fetch the node id.
func TestEntitySource_IDPinnedOnce(t *testing.T) {
	calls := 0
	s := testEntitySource(
		"",
		func() string { calls++; return "node-xyz" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	for i := 0; i < 5; i++ {
		_, ok := s.Observe()
		if !ok {
			t.Fatalf("Observe %d returned ok=false", i)
		}
	}
	if calls != 1 {
		t.Errorf("fetchNodeID called %d times across 5 Observe calls; want exactly 1", calls)
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that a "monitors" relation
// is included when the agent id is available.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := testEntitySource(
		"",
		func() string { return "node-abc" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	rel, found := findRelation(obs, "monitors")
	if !found {
		t.Fatalf("no monitors relation; got %v", relTypes(obs))
	}
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if rel.ToType != "service.instance" {
		t.Errorf("ToType = %q, want %q", rel.ToType, "service.instance")
	}
	fromID := rel.FromID["service.instance.id"]
	if fromID != "agent-instance-001" {
		t.Errorf("FromID[service.instance.id] = %q, want %q", fromID, "agent-instance-001")
	}
	toID := rel.ToID["service.instance.id"]
	if toID != "envoy:node-abc" {
		t.Errorf("ToID[service.instance.id] = %q, want %q", toID, "envoy:node-abc")
	}
}

// TestEntitySource_MonitorsEdgeAbsent verifies that no "monitors" relation is
// emitted when the agent id is not available (agentstate returns "").
func TestEntitySource_MonitorsEdgeAbsent(t *testing.T) {
	// Ensure agent id is empty for this test.
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	s := testEntitySource(
		"",
		func() string { return "node-abc" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	// No monitors edge when the agent id is empty. A runs_on edge may still be
	// present (the loopback endpoint) — assert on the monitors type, not the count.
	if _, found := findRelation(obs, "monitors"); found {
		t.Errorf("monitors relation must be absent when agent id is empty; got %v", relTypes(obs))
	}
}

// relTypes lists the relation types in an observation.
func relTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}

// findRelation returns the first relation of the given type.
func findRelation(obs entity.Observation, relType string) (entity.Relation, bool) {
	for _, r := range obs.Relations {
		if r.Type == relType {
			return r, true
		}
	}
	return entity.Relation{}, false
}

// TestEntitySource_LocalRunsOn verifies a loopback-monitored envoy emits a
// runs_on→host edge (so it does not float), and a remote-monitored one does not.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback endpoint → runs_on present, targeting the agent host.
	local := testEntitySource("", func() string { return "node-abc" }, func() string { return "H" })
	local.setReachable(true)
	obs, _ := local.Observe()
	runsOn, found := findRelation(obs, "runs_on")
	if !found {
		t.Fatalf("loopback envoy: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "envoy:node-abc" {
		t.Errorf("runs_on source = %v, want envoy:node-abc", runsOn.FromID)
	}

	// Remote endpoint → no runs_on.
	remote := testEntitySource("", func() string { return "node-abc" }, func() string { return "H" })
	remote.serverAddr = "10.0.0.5"
	remote.setReachable(true)
	robs, _ := remote.Observe()
	if _, found := findRelation(robs, "runs_on"); found {
		t.Errorf("remote envoy must NOT emit runs_on; relations=%v", relTypes(robs))
	}
}

// TestFetchEnvoyNodeID_Integration verifies the /server_info JSON parsing
// against a stubbed HTTP server (no live Envoy needed).
func TestFetchEnvoyNodeID_Integration(t *testing.T) {
	const serverInfoJSON = `{
		"version": "1.30.0",
		"state": "LIVE",
		"hot_restart_version": "disabled",
		"node": {
			"id": "edge-proxy-eu-west-1",
			"cluster": "production",
			"metadata": {}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/server_info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(serverInfoJSON)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	client := &http.Client{}
	got, version := fetchEnvoyServerInfo(client, srv.URL)
	if got != "edge-proxy-eu-west-1" {
		t.Errorf("fetchEnvoyServerInfo node id = %q, want %q", got, "edge-proxy-eu-west-1")
	}
	if version != "1.30.0" {
		t.Errorf("fetchEnvoyServerInfo version = %q, want 1.30.0", version)
	}
}

// TestFetchEnvoyNodeID_EmptyNodeID verifies that an empty node.id in the
// response is returned as "" (so the caller falls through to the fallback).
func TestFetchEnvoyNodeID_EmptyNodeID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"node":{"id":""}}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	got, _ := fetchEnvoyServerInfo(&http.Client{}, srv.URL)
	if got != "" {
		t.Errorf("fetchEnvoyServerInfo node id = %q, want empty string", got)
	}
}

// TestEntitySource_ServiceVersion verifies the /server_info version rides the
// entity as service.version (toise#216 AT1).
func TestEntitySource_ServiceVersion(t *testing.T) {
	s := &envoyEntitySource{
		descAttr:  map[string]any{"service.name": "envoy"},
		fetchInfo: func() (string, string) { return "node-1", "1.30.0" },
		hostID:    func() string { return "h" },
	}
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe ok=false")
	}
	if got := obs.Entities[0].Attributes["service.version"]; got != "1.30.0" {
		t.Errorf("service.version = %v, want 1.30.0", got)
	}
}

// TestEntitySource_DescriptiveAttrs verifies that server.address and
// server.port (as int64) are present in the emitted entity.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	s := testEntitySource(
		"",
		func() string { return "node-x" },
		func() string { return "host-uuid" },
	)
	s.setReachable(true)

	obs, ok := s.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if attrs["service.name"] != "envoy" {
		t.Errorf("service.name = %v, want %q", attrs["service.name"], "envoy")
	}
	if attrs["server.address"] != "127.0.0.1" {
		t.Errorf("server.address = %v, want %q", attrs["server.address"], "127.0.0.1")
	}
	if attrs["server.port"] != int64(9901) {
		t.Errorf("server.port = %v (%T), want int64(9901)", attrs["server.port"], attrs["server.port"])
	}
}

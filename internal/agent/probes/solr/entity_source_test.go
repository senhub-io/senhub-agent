package solr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// clusterStatusPayload returns a minimal CLUSTERSTATUS response containing a
// cluster id. Real Solr 8+ includes cluster.properties.id in this endpoint.
func clusterStatusPayload(clusterID string) map[string]interface{} {
	return map[string]interface{}{
		"cluster": map[string]interface{}{
			"properties": map[string]interface{}{
				"id": clusterID,
			},
		},
	}
}

// newTestHTTPClient returns an *http.Client suitable for unit tests (no
// timeouts that interfere with httptest).
func newTestHTTPClient() *http.Client {
	return &http.Client{}
}

// TestEntitySource_InstanceNameOverride verifies that when instance_name is
// set, the entity is emitted immediately with that verbatim id and the
// CLUSTERSTATUS endpoint is never consulted.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	// CLUSTERSTATUS server that must never be called.
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(clusterStatusPayload("should-not-be-used"))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "my-prod-solr", client)
	src.setReachable(true, "9.0.0")
	// tryPinClusterIDOrHostPort must be a no-op when already pinned.
	src.tryPinClusterIDOrHostPort(context.Background())

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["db.instance.id"]
	if gotID != "my-prod-solr" {
		t.Errorf("db.instance.id = %v, want my-prod-solr", gotID)
	}
	if called {
		t.Error("CLUSTERSTATUS was called despite instance_name being set")
	}
}

// TestEntitySource_TechIDPinned verifies that when CLUSTERSTATUS returns a
// cluster id, the entity id is "solr:<cluster-id>".
func TestEntitySource_TechIDPinned(t *testing.T) {
	const wantClusterID = "abc-123-xyz"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clusterStatusPayload(wantClusterID))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	src.setReachable(true, "8.11.2")
	src.tryPinClusterIDOrHostPort(context.Background())

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after cluster id pinned")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	gotID := obs.Entities[0].ID["db.instance.id"]
	want := "solr:" + wantClusterID
	if gotID != want {
		t.Errorf("db.instance.id = %v, want %v", gotID, want)
	}
	// Descriptive attrs must be present.
	attrs := obs.Entities[0].Attributes
	if attrs["db.system.name"] != "solr" {
		t.Errorf("db.system.name = %v, want solr", attrs["db.system.name"])
	}
	if attrs["server.address"] == nil {
		t.Error("server.address missing from attributes")
	}
	if attrs["server.port"] == nil {
		t.Error("server.port missing from attributes")
	}
}

// TestEntitySource_NotEmittedBeforePinned verifies that the entity is NOT
// emitted (ok=false) before a tech id is pinned (i.e. before
// tryPinClusterIDOrHostPort has been called while reachable).
func TestEntitySource_NotEmittedBeforePinned(t *testing.T) {
	// Server that blocks or returns slowly — we never call the method.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clusterStatusPayload("cluster-999"))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	// Mark as reachable but do NOT call tryPinClusterIDOrHostPort.
	src.setReachable(true, "9.0.0")

	_, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true before id was pinned, want false")
	}
}

// TestEntitySource_HostPortFallback verifies that when CLUSTERSTATUS is
// unavailable (standalone Solr), the entity id falls back to host:port.
func TestEntitySource_HostPortFallback(t *testing.T) {
	// Server that returns a non-200 (standalone Solr rejects the collections API).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	src.setReachable(true, "")
	src.tryPinClusterIDOrHostPort(context.Background())

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after host:port fallback pinned")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	gotID, _ := obs.Entities[0].ID["db.instance.id"].(string)
	// Expect "host:port" — parse the server's addr from the test server URL.
	_, port := hostPort(srv.URL)
	want := fmt.Sprintf("127.0.0.1:%d", port)
	if gotID != want {
		t.Errorf("db.instance.id = %q, want %q", gotID, want)
	}
}

// TestEntitySource_MonitorsEdge_Present verifies that a monitors relation is
// included in the observation when the agent instance id is set.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	const agentID = "test-agent-42"
	agentstate.SetAgentInstanceID(agentID)
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Use a server returning a cluster id.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clusterStatusPayload("cluster-edge-test"))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	src.setReachable(true, "")
	src.tryPinClusterIDOrHostPort(context.Background())

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("got %d relations, want 1", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want monitors", rel.Type)
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", rel.FromType)
	}
	gotFrom := rel.FromID["service.instance.id"]
	if gotFrom != agentID {
		t.Errorf("FromID service.instance.id = %v, want %v", gotFrom, agentID)
	}
	if rel.ToType != "db" {
		t.Errorf("ToType = %q, want db", rel.ToType)
	}
	gotTo := rel.ToID["db.instance.id"]
	if gotTo != "solr:cluster-edge-test" {
		t.Errorf("ToID db.instance.id = %v, want solr:cluster-edge-test", gotTo)
	}
}

// TestEntitySource_MonitorsEdge_Absent verifies that when the agent instance
// id is not set, the monitors relation is NOT emitted.
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clusterStatusPayload("cluster-no-edge"))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	src.setReachable(true, "")
	src.tryPinClusterIDOrHostPort(context.Background())

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("got %d relations, want 0 (no agent id set)", len(obs.Relations))
	}
}

// TestEntitySource_IDImmutable verifies that a pinned id is never changed by a
// subsequent tryPinClusterIDOrHostPort call that would return a different id.
func TestEntitySource_IDImmutable(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Return different ids on each call to prove the second is ignored.
		_ = json.NewEncoder(w).Encode(clusterStatusPayload(fmt.Sprintf("cluster-%d", calls)))
	}))
	defer srv.Close()

	client := newTestHTTPClient()
	src := newSolrEntitySource(srv.URL, "", client)
	src.setReachable(true, "")

	src.tryPinClusterIDOrHostPort(context.Background())
	obs1, _ := src.Observe()
	id1 := obs1.Entities[0].ID["db.instance.id"]

	src.tryPinClusterIDOrHostPort(context.Background())
	obs2, _ := src.Observe()
	id2 := obs2.Entities[0].ID["db.instance.id"]

	if id1 != id2 {
		t.Errorf("id changed between calls: %v → %v (must be immutable once pinned)", id1, id2)
	}
	// The server was called exactly once (second call skipped because already pinned).
	if calls != 1 {
		t.Errorf("CLUSTERSTATUS called %d times, want 1", calls)
	}
}

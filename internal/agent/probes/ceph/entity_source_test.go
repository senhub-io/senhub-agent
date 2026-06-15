package ceph

import (
	"errors"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// stubFetch returns a fetchFsidFunc that always returns (fsid, nil) when fsid
// is non-empty, or an error when fsid is "".
func stubFetch(fsid string) fetchFsidFunc {
	if fsid == "" {
		return func() (string, error) {
			return "", errors.New("fsid unavailable")
		}
	}
	return func() (string, error) { return fsid, nil }
}

// stubHostID returns a hostIDFunc that always returns hid.
func stubHostID(hid string) hostIDFunc {
	return func() string { return hid }
}

// TestCephEntitySource_InstanceNameOverride verifies that instance_name is
// pinned at construction and Observe returns the entity immediately.
func TestCephEntitySource_InstanceNameOverride(t *testing.T) {
	src := newCephEntitySource(
		"my-ceph-cluster",
		"https://ceph.example.com:8443",
		stubFetch(""),       // should never be called
		stubHostID("h-001"), // should never be called
	)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; instance_name pin should be immediate")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(Entities) = %d; want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != entityTypeServiceInstance {
		t.Errorf("entity type = %q; want %q", e.Type, entityTypeServiceInstance)
	}
	gotID, ok := e.ID[idKeyServiceInstanceID]
	if !ok {
		t.Fatalf("entity ID missing key %q", idKeyServiceInstanceID)
	}
	if gotID != "my-ceph-cluster" {
		t.Errorf("entity id = %q; want %q", gotID, "my-ceph-cluster")
	}
}

// TestCephEntitySource_NotEmittedBeforePinned verifies that Observe returns
// ok=false before pinID() is called (no instance_name, tech id not yet
// fetched).
func TestCephEntitySource_NotEmittedBeforePinned(t *testing.T) {
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch("deadbeef-1234-5678-abcd-ef0123456789"),
		stubHostID("h-001"),
	)

	obs, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true before pinID(); want ok=false")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("Observe returned %d entities before pinID(); want 0", len(obs.Entities))
	}
}

// TestCephEntitySource_TechIDPinned verifies that pinID() fetches the cluster
// fsid and pins "ceph:<fsid>".
func TestCephEntitySource_TechIDPinned(t *testing.T) {
	const fsid = "deadbeef-1234-5678-abcd-ef0123456789"
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch(fsid),
		stubHostID("h-001"),
	)

	src.pinID()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after pinID(); want ok=true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(Entities) = %d; want 1", len(obs.Entities))
	}
	wantID := "ceph:" + fsid
	gotID := obs.Entities[0].ID[idKeyServiceInstanceID]
	if gotID != wantID {
		t.Errorf("entity id = %q; want %q", gotID, wantID)
	}
}

// TestCephEntitySource_FallbackHostID verifies that when the fsid fetch fails,
// the source falls back to "ceph@<host.id>".
func TestCephEntitySource_FallbackHostID(t *testing.T) {
	const hid = "machine-uuid-0001"
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch(""), // fsid unavailable
		stubHostID(hid),
	)

	src.pinID()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after fallback pin; want ok=true")
	}
	wantID := "ceph@" + hid
	gotID := obs.Entities[0].ID[idKeyServiceInstanceID]
	if gotID != wantID {
		t.Errorf("entity id = %q; want %q", gotID, wantID)
	}
}

// TestCephEntitySource_FallbackLastResort verifies that when both fsid and
// host-id are unavailable, the id degrades to the bare "ceph".
func TestCephEntitySource_FallbackLastResort(t *testing.T) {
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch(""),  // fsid unavailable
		stubHostID(""), // host id unavailable
	)

	src.pinID()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after last-resort pin; want ok=true")
	}
	gotID := obs.Entities[0].ID[idKeyServiceInstanceID]
	if gotID != "ceph" {
		t.Errorf("entity id = %q; want \"ceph\"", gotID)
	}
}

// TestCephEntitySource_IDImmutable verifies that once pinned, repeated calls
// to pinID() and Observe() return the same id.
func TestCephEntitySource_IDImmutable(t *testing.T) {
	const fsid = "stable-uuid-0001"
	callCount := 0
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		func() (string, error) {
			callCount++
			return fsid, nil
		},
		stubHostID("h-001"),
	)

	src.pinID()
	src.pinID() // second call must be a no-op

	obs1, _ := src.Observe()
	obs2, _ := src.Observe()

	if obs1.Entities[0].ID[idKeyServiceInstanceID] != obs2.Entities[0].ID[idKeyServiceInstanceID] {
		t.Error("entity id changed between Observe() calls")
	}
	if callCount > 1 {
		t.Errorf("fetchFsid called %d times; want 1 (pinned after first)", callCount)
	}
}

// TestCephEntitySource_MonitorsEdge_Present verifies that the monitors
// relation is emitted when the agent id is set.
func TestCephEntitySource_MonitorsEdge_Present(t *testing.T) {
	const agentID = "agent-instance-001"
	agentstate.SetAgentInstanceID(agentID)
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	const fsid = "cluster-uuid-0001"
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch(fsid),
		stubHostID("h-001"),
	)

	src.pinID()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("len(Relations) = %d; want 1", len(obs.Relations))
	}

	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q; want \"monitors\"", rel.Type)
	}
	if rel.FromType != entityTypeServiceInstance {
		t.Errorf("FromType = %q; want %q", rel.FromType, entityTypeServiceInstance)
	}
	if rel.ToType != entityTypeServiceInstance {
		t.Errorf("ToType = %q; want %q", rel.ToType, entityTypeServiceInstance)
	}
	if rel.FromID[idKeyServiceInstanceID] != agentID {
		t.Errorf("FromID = %v; want agent id %q", rel.FromID, agentID)
	}
	wantTargetID := "ceph:" + fsid
	if rel.ToID[idKeyServiceInstanceID] != wantTargetID {
		t.Errorf("ToID = %v; want %q", rel.ToID, wantTargetID)
	}
}

// TestCephEntitySource_MonitorsEdge_Absent verifies that the monitors
// relation is NOT emitted when the agent id is empty (entity emission disabled
// or not yet started).
func TestCephEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	const fsid = "cluster-uuid-0002"
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		stubFetch(fsid),
		stubHostID("h-001"),
	)

	src.pinID()

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("len(Relations) = %d; want 0 when agent id is empty", len(obs.Relations))
	}
}

// TestCephEntitySource_DescriptiveAttrs verifies that server.address and
// server.port are set as descriptive attributes (not identity).
func TestCephEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newCephEntitySource(
		"my-cluster",
		"https://ceph.example.com:8443",
		stubFetch(""),
		stubHostID(""),
	)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	e := obs.Entities[0]

	if e.Attributes["service.name"] != "ceph" {
		t.Errorf("service.name = %v; want \"ceph\"", e.Attributes["service.name"])
	}
	if e.Attributes["server.address"] != "ceph.example.com" {
		t.Errorf("server.address = %v; want \"ceph.example.com\"", e.Attributes["server.address"])
	}
	if e.Attributes["server.port"] != "8443" {
		t.Errorf("server.port = %v; want \"8443\"", e.Attributes["server.port"])
	}
}

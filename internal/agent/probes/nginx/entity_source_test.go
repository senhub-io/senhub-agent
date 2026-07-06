package nginx

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestEntitySource_IDFromHostID verifies that when no instance_name is set the
// entity id is "nginx@<hostID>" (stable, non-network-derived).
func TestEntitySource_IDFromHostID(t *testing.T) {
	src := newNginxEntitySource("http://localhost/nginx_status", "", "test-host-uuid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	want := "nginx@test-host-uuid"
	if got != want {
		t.Errorf("service.instance.id = %q, want %q", got, want)
	}
}

// TestNginxVersionFromServerHeader covers the Server-header parser.
func TestNginxVersionFromServerHeader(t *testing.T) {
	cases := map[string]string{
		"nginx/1.27.0": "1.27.0",
		"nginx":        "", // server_tokens off
		"":             "",
		"Apache/2.4":   "",
	}
	for header, want := range cases {
		if got := nginxVersionFromServerHeader(header); got != want {
			t.Errorf("nginxVersionFromServerHeader(%q) = %q, want %q", header, got, want)
		}
	}
}

// TestEntitySource_ServiceVersion verifies setVersion surfaces service.version
// on the entity (toise#216 AT1), absent until set.
func TestEntitySource_ServiceVersion(t *testing.T) {
	src := newNginxEntitySource("http://localhost/nginx_status", "", "h")
	src.setReachable(true)

	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["service.version"]; has {
		t.Error("service.version must be absent before it is set")
	}

	src.setVersion("1.27.0")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["service.version"]; got != "1.27.0" {
		t.Errorf("service.version = %v, want 1.27.0", got)
	}
}

// TestEntitySource_IDFromInstanceName verifies that a configured instance_name
// overrides the host-id-derived default.
func TestEntitySource_IDFromInstanceName(t *testing.T) {
	src := newNginxEntitySource("http://localhost/nginx_status", "my-nginx", "test-host-uuid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	want := "my-nginx"
	if got != want {
		t.Errorf("service.instance.id = %q, want %q (instance_name should win)", got, want)
	}
}

// TestEntitySource_IDFallback verifies the last-resort "nginx" id when both
// instance_name and hostID are empty.
func TestEntitySource_IDFallback(t *testing.T) {
	src := newNginxEntitySource("http://localhost/nginx_status", "", "")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true")
	}
	got, _ := obs.Entities[0].ID["service.instance.id"].(string)
	if got != "nginx" {
		t.Errorf("service.instance.id = %q, want %q (fallback)", got, "nginx")
	}
}

// TestEntitySource_IDNeverURL verifies that the entity id is never a URL or
// contains "://" (the previous incorrect behaviour).
func TestEntitySource_IDNeverURL(t *testing.T) {
	endpoints := []string{
		"http://192.0.2.1:8080/nginx_status",
		"https://nginx.example.com/nginx_status",
		"http://localhost/nginx_status",
	}
	for _, ep := range endpoints {
		src := newNginxEntitySource(ep, "", "some-host-id")
		src.setReachable(true)
		obs, _ := src.Observe()
		id, _ := obs.Entities[0].ID["service.instance.id"].(string)
		for _, bad := range []string{"://", "http", "https"} {
			if contains(id, bad) {
				t.Errorf("endpoint %q produced id %q containing %q — id must not be URL-derived", ep, id, bad)
			}
		}
	}
}

// TestEntitySource_DescriptiveAttrsPreserved verifies that server.address and
// server.port remain as descriptive attributes even though they are no longer
// part of the identity.
func TestEntitySource_DescriptiveAttrsPreserved(t *testing.T) {
	src := newNginxEntitySource("http://192.0.2.5:9090/nginx_status", "", "hid")
	src.setReachable(true)

	obs, _ := src.Observe()
	attrs := obs.Entities[0].Attributes
	if attrs["server.address"] != "192.0.2.5" {
		t.Errorf("server.address = %v, want %q", attrs["server.address"], "192.0.2.5")
	}
	if attrs["server.port"] != int64(9090) {
		t.Errorf("server.port = %v, want 9090", attrs["server.port"])
	}
	if attrs["service.name"] != "nginx" {
		t.Errorf("service.name = %v, want %q", attrs["service.name"], "nginx")
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that a monitors relation is
// emitted from the agent to the nginx instance when an agent id is set.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-abc")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newNginxEntitySource("http://localhost/nginx_status", "", "hid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	var rel *entity.Relation
	for i := range obs.Relations {
		if obs.Relations[i].Type == "monitors" {
			rel = &obs.Relations[i]
		}
	}
	if rel == nil {
		t.Fatalf("no monitors relation; got %v", relTypes(obs))
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want service.instance", rel.FromType)
	}
	if rel.ToType != "service.instance" {
		t.Errorf("ToType = %q, want service.instance", rel.ToType)
	}
	fromID, _ := rel.FromID["service.instance.id"].(string)
	if fromID != "agent-abc" {
		t.Errorf("FromID[service.instance.id] = %q, want %q", fromID, "agent-abc")
	}
	toID, _ := rel.ToID["service.instance.id"].(string)
	if toID != "nginx@hid" {
		t.Errorf("ToID[service.instance.id] = %q, want %q", toID, "nginx@hid")
	}
}

// TestEntitySource_MonitorsEdgeAbsent verifies that no monitors relation is
// emitted when the agent id is not set (entity foundation off or not yet
// resolved).
func TestEntitySource_MonitorsEdgeAbsent(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newNginxEntitySource("http://localhost/nginx_status", "", "hid")
	src.setReachable(true)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	// No monitors edge when the agent id is empty. A runs_on edge may still be
	// present (the localhost endpoint is loopback) — that is independent of the
	// agent id, so assert specifically on the monitors type, not the count.
	for _, ty := range relTypes(obs) {
		if ty == "monitors" {
			t.Errorf("monitors relation must be absent when agent id empty; got %v", relTypes(obs))
		}
	}
}

// TestEntitySource_ObserveDownReturnsFalse verifies that a down endpoint
// causes Observe to return ok=false.
func TestEntitySource_ObserveDownReturnsFalse(t *testing.T) {
	src := newNginxEntitySource("http://localhost/nginx_status", "", "hid")
	// default: up=false
	_, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true before any setReachable(true), want false")
	}
}

// TestEntitySource_ObserveDown_NoRelation verifies that when down, no entity
// or relation is emitted (not even a stale one).
func TestEntitySource_ObserveDown_NoRelation(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-abc")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newNginxEntitySource("http://localhost/nginx_status", "", "hid")
	src.setReachable(false)

	obs, ok := src.Observe()
	if ok {
		t.Error("Observe returned ok=true when down")
	}
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("got non-empty observation when down: %+v", obs)
	}
}

// foldRelationships is tested on the entity package side; here we only need
// to verify the monitors edge uses Relation (not Relationship) — the type
// system already enforces this, but let's confirm the correct field is set.
func TestEntitySource_MonitorsEdgeType(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-x")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newNginxEntitySource("http://localhost/nginx_status", "my-nginx", "")
	src.setReachable(true)

	obs, _ := src.Observe()
	var found bool
	for _, r := range obs.Relations {
		if r.Type == "monitors" {
			found = true
			// FromID must reference the agent, ToID the nginx instance.
			if r.FromID["service.instance.id"] != "agent-x" {
				t.Errorf("FromID wrong: %v", r.FromID)
			}
			if r.ToID["service.instance.id"] != "my-nginx" {
				t.Errorf("ToID wrong: %v", r.ToID)
			}
		}
	}
	if !found {
		t.Error("monitors relation not found in observation")
	}
}

// contains is a simple helper — strings.Contains would add an import cycle
// risk; plain loop is cleaner for this package.
func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

// Compile-time check: the entity.Relation type is used, not entity.Relationship.
var _ entity.Relation = entity.Relation{}

// relTypes lists the relation types in an observation.
func relTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}

// TestEntitySource_LocalRunsOn verifies a loopback-monitored nginx emits a
// runs_on→host edge (so it does not float), and a remote-monitored one does not.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	t.Setenv("dummy", "1")
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback endpoint → runs_on present, targeting the agent host.
	local := newNginxEntitySource("http://127.0.0.1:8080/nginx_status", "", "H")
	local.setReachable(true)
	obs, _ := local.Observe()
	var runsOn *entity.Relation
	for i := range obs.Relations {
		if obs.Relations[i].Type == "runs_on" {
			runsOn = &obs.Relations[i]
		}
	}
	if runsOn == nil {
		t.Fatalf("loopback nginx: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "nginx@H" {
		t.Errorf("runs_on source = %v, want nginx@H", runsOn.FromID)
	}

	// Remote endpoint → no runs_on (must not claim to run on the agent host).
	remote := newNginxEntitySource("http://10.0.0.5:8080/nginx_status", "", "H")
	remote.setReachable(true)
	robs, _ := remote.Observe()
	for _, ty := range relTypes(robs) {
		if ty == "runs_on" {
			t.Errorf("remote nginx must NOT emit runs_on; relations=%v", relTypes(robs))
		}
	}
}

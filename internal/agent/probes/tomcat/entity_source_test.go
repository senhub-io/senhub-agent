package tomcat

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// relByType returns a pointer to the first relation of the given type, or nil.
func relByType(obs entity.Observation, ty string) *entity.Relation {
	for i := range obs.Relations {
		if obs.Relations[i].Type == ty {
			return &obs.Relations[i]
		}
	}
	return nil
}

// relTypes lists the relation types in an observation.
func relTypes(obs entity.Observation) []string {
	var ts []string
	for _, r := range obs.Relations {
		ts = append(ts, r.Type)
	}
	return ts
}

// TestResolveInstanceID verifies the stable-id precedence rule.
func TestResolveInstanceID_InstanceNameOverrides(t *testing.T) {
	got := resolveInstanceID("my-tomcat", "host-uuid-123")
	if got != "my-tomcat" {
		t.Errorf("resolveInstanceID with instance_name = %q, want \"my-tomcat\"", got)
	}
}

func TestResolveInstanceID_HostIDFallback(t *testing.T) {
	got := resolveInstanceID("", "host-uuid-abc")
	want := "tomcat@host-uuid-abc"
	if got != want {
		t.Errorf("resolveInstanceID without instance_name = %q, want %q", got, want)
	}
}

func TestResolveInstanceID_LastResort(t *testing.T) {
	got := resolveInstanceID("", "")
	if got != "tomcat" {
		t.Errorf("resolveInstanceID with no host id = %q, want \"tomcat\"", got)
	}
}

// TestTomcatEntitySource_InstanceName checks that instance_name is used verbatim
// as service.instance.id when provided.
func TestTomcatEntitySource_InstanceName(t *testing.T) {
	src := newTomcatEntitySource("prod-tomcat-1", "some-host-id", "localhost", 8080)
	src.SetUp(true, nil)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true when up")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe: want 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "service.instance" {
		t.Errorf("entity type = %q, want \"service.instance\"", e.Type)
	}
	id, _ := e.ID["service.instance.id"].(string)
	if id != "prod-tomcat-1" {
		t.Errorf("service.instance.id = %q, want \"prod-tomcat-1\"", id)
	}
}

// TestTomcatEntitySource_HostIDFallback checks that "tomcat@<hostid>" is used
// when no instance_name is configured (hermetic: injected stub host id).
func TestTomcatEntitySource_HostIDFallback(t *testing.T) {
	src := newTomcatEntitySource("", "stub-machine-id-42", "192.168.1.10", 8080)
	src.SetUp(true, nil)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true when up")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe: want 1 entity, got %d", len(obs.Entities))
	}
	id, _ := obs.Entities[0].ID["service.instance.id"].(string)
	want := "tomcat@stub-machine-id-42"
	if id != want {
		t.Errorf("service.instance.id = %q, want %q", id, want)
	}
}

// TestTomcatEntitySource_DescriptiveAttrs checks that server.address and
// server.port are present as descriptive attributes (not identity) and that
// service.name is "tomcat".
func TestTomcatEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newTomcatEntitySource("", "hid", "myhost.example.com", 9090)
	src.SetUp(true, nil)

	obs, _ := src.Observe()
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe: want 1 entity, got %d", len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if attrs["service.name"] != "tomcat" {
		t.Errorf("service.name = %q, want \"tomcat\"", attrs["service.name"])
	}
	if attrs["server.address"] != "myhost.example.com" {
		t.Errorf("server.address = %q, want \"myhost.example.com\"", attrs["server.address"])
	}
	if attrs["server.port"] != int64(9090) {
		t.Errorf("server.port = %v, want 9090", attrs["server.port"])
	}
}

// TestTomcatEntitySource_ServiceVersion verifies that a version passed via
// SetUp is merged onto the descriptive attribute set and surfaces in Observe.
func TestTomcatEntitySource_ServiceVersion(t *testing.T) {
	src := newTomcatEntitySource("", "hid", "myhost.example.com", 9090)
	src.SetUp(true, map[string]any{"service.version": "9.0.71"})

	obs, _ := src.Observe()
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe: want 1 entity, got %d", len(obs.Entities))
	}
	attrs := obs.Entities[0].Attributes
	if attrs["service.version"] != "9.0.71" {
		t.Errorf("service.version = %v, want \"9.0.71\"", attrs["service.version"])
	}
	// Descriptive attrs must remain intact alongside the version.
	if attrs["service.name"] != "tomcat" {
		t.Errorf("service.name = %q, want \"tomcat\"", attrs["service.name"])
	}
}

// TestTomcatEntitySource_NoVersionWhenAbsent verifies that service.version is
// omitted when SetUp is called without it (read failed or attribute empty).
func TestTomcatEntitySource_NoVersionWhenAbsent(t *testing.T) {
	src := newTomcatEntitySource("", "hid", "myhost.example.com", 9090)
	src.SetUp(true, nil)

	obs, _ := src.Observe()
	if _, ok := obs.Entities[0].Attributes["service.version"]; ok {
		t.Error("service.version present, want omitted")
	}
}

func TestParseTomcatVersion(t *testing.T) {
	cases := map[string]string{
		"Apache Tomcat/9.0.71":  "9.0.71",
		"Apache Tomcat/10.1.7":  "10.1.7",
		"  Apache Tomcat/8.5  ": "8.5",
		"11.0.0":                "11.0.0",
		"":                      "",
	}
	for in, want := range cases {
		if got := parseTomcatVersion(in); got != want {
			t.Errorf("parseTomcatVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTomcatEntitySource_IDDoesNotContainNetwork checks that the stable id
// never contains a URL, port, or IP-derived component when using the host-id path.
func TestTomcatEntitySource_IDDoesNotContainNetwork(t *testing.T) {
	src := newTomcatEntitySource("", "machine-uuid", "10.0.0.1", 8080)
	src.SetUp(true, nil)
	obs, _ := src.Observe()
	id, _ := obs.Entities[0].ID["service.instance.id"].(string)
	for _, bad := range []string{"://", "10.0.0.1", "8080", "http"} {
		if contains(id, bad) {
			t.Errorf("service.instance.id %q must not contain network-derived %q", id, bad)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestTomcatEntitySource_MonitorsEdge_Present checks that when the agent id is
// set, Observe includes a monitors relation from the agent to the target.
func TestTomcatEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-instance-xyz")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newTomcatEntitySource("my-app", "", "localhost", 8080)
	src.SetUp(true, nil)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true")
	}
	// A loopback target may also yield a runs_on edge; assert by type, not count.
	rel := relByType(obs, "monitors")
	if rel == nil {
		t.Fatalf("no monitors relation; got %v", relTypes(obs))
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want \"service.instance\"", rel.FromType)
	}
	fromID, _ := rel.FromID["service.instance.id"].(string)
	if fromID != "agent-instance-xyz" {
		t.Errorf("From service.instance.id = %q, want \"agent-instance-xyz\"", fromID)
	}
	if rel.ToType != "service.instance" {
		t.Errorf("ToType = %q, want \"service.instance\"", rel.ToType)
	}
	toID, _ := rel.ToID["service.instance.id"].(string)
	if toID != "my-app" {
		t.Errorf("To service.instance.id = %q, want \"my-app\"", toID)
	}
}

// TestTomcatEntitySource_MonitorsEdge_Absent checks that when the agent id is
// empty (entity emission off), no monitors relation is emitted.
func TestTomcatEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	src := newTomcatEntitySource("my-app", "", "localhost", 8080)
	src.SetUp(true, nil)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true")
	}
	// A runs_on edge may still be present (loopback target); only the monitors
	// edge must be absent when the agent id is empty.
	if relByType(obs, "monitors") != nil {
		t.Errorf("monitors relation must be absent when agent id is empty; got %v", relTypes(obs))
	}
}

// TestTomcatEntitySource_LocalRunsOn verifies a loopback-monitored Tomcat emits
// a runs_on→host edge (its host-scoped id carries no loopback literal), and a
// remote-monitored one does not.
func TestTomcatEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-1")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Loopback endpoint → runs_on present, targeting the agent host.
	local := newTomcatEntitySource("", "H", "127.0.0.1", 8080)
	local.SetUp(true, nil)
	obs, _ := local.Observe()
	runsOn := relByType(obs, "runs_on")
	if runsOn == nil {
		t.Fatalf("loopback tomcat: expected a runs_on edge, got relations %v", relTypes(obs))
	}
	if runsOn.ToType != "host" || runsOn.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", runsOn.ToType, runsOn.ToID)
	}
	if runsOn.FromID["service.instance.id"] != "tomcat@H" {
		t.Errorf("runs_on source = %v, want tomcat@H", runsOn.FromID)
	}

	// Remote endpoint → no runs_on (must not claim to run on the agent host).
	remote := newTomcatEntitySource("", "H", "10.0.0.5", 8080)
	remote.SetUp(true, nil)
	robs, _ := remote.Observe()
	if relByType(robs, "runs_on") != nil {
		t.Errorf("remote tomcat must NOT emit runs_on; relations=%v", relTypes(robs))
	}
}

// TestTomcatEntitySource_Down checks that Observe returns ok=false when the
// target is unreachable (D3: keep consumer's last-known state).
func TestTomcatEntitySource_Down(t *testing.T) {
	src := newTomcatEntitySource("my-app", "hid", "localhost", 8080)
	src.SetUp(false, nil)

	_, ok := src.Observe()
	if ok {
		t.Error("Observe: want ok=false when target is down")
	}
}

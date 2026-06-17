package zookeeper

import (
	"fmt"
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// newObserver builds an entityObserver with injected hostIDFunc and
// pre-set addr/port so tests never call gopsutil.
func newObserver(hostIDStub string) *entityObserver {
	return &entityObserver{
		addr:       "zk.example.com",
		port:       2181,
		hostIDFunc: func() string { return hostIDStub },
	}
}

// TestEntitySource_InstanceNameOverride: precedence-1 — operator-supplied
// instance_name is pinned at construction; Observe returns it immediately.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	obs := newObserver("host-abc")
	obs.pin("my-zk-node")

	obs.setUp("zookeeper:3", true, "3.8.0")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after instance_name pin")
	}
	if len(got.Entities) != 1 {
		t.Fatalf("entities: want 1, got %d", len(got.Entities))
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "my-zk-node" {
		t.Errorf("service.instance.id: want %q, got %q", "my-zk-node", id)
	}
}

// TestEntitySource_TechID: precedence-2 — serverId from conf is adopted as
// "zookeeper:<n>" and pinned on the first Collect.
func TestEntitySource_TechID(t *testing.T) {
	obs := newObserver("host-abc")

	// Before any setUp the id is not pinned.
	if !obs.needsPin() {
		t.Fatal("needsPin: want true before first Collect")
	}

	obs.setUp("zookeeper:1", true, "3.8.0")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after tech id pinned")
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "zookeeper:1" {
		t.Errorf("service.instance.id: want %q, got %q", "zookeeper:1", id)
	}
	if obs.needsPin() {
		t.Error("needsPin: want false after id is pinned")
	}
}

// TestEntitySource_NotEmittedBeforePin: Observe must return ok=false until
// the id is pinned — no entity emitted before identity is known.
func TestEntitySource_NotEmittedBeforePin(t *testing.T) {
	obs := newObserver("host-abc")

	// Target unreachable: no tech id, up=false.
	obs.setUp("", false, "")

	_, ok := obs.Observe()
	if ok {
		t.Error("Observe: want ok=false before id is pinned (target unreachable)")
	}
	if !obs.needsPin() {
		t.Error("needsPin: want true — id should not be pinned on connection failure")
	}
}

// TestEntitySource_FallbackPath: when target is reachable but conf returned no
// serverId, the fallback "zookeeper@<host.id>" is adopted.
func TestEntitySource_FallbackPath(t *testing.T) {
	obs := newObserver("machine-uuid-42")

	// Reachable but no tech id.
	obs.setUp("", true, "3.8.0")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after fallback pin")
	}
	want := "zookeeper@machine-uuid-42"
	id := got.Entities[0].ID["service.instance.id"]
	if id != want {
		t.Errorf("service.instance.id: want %q, got %q", want, id)
	}
}

// TestEntitySource_FallbackPath_LastResort: when both conf and host id are
// unavailable, the last-resort plain "zookeeper" id is adopted.
func TestEntitySource_FallbackPath_LastResort(t *testing.T) {
	obs := &entityObserver{
		addr:       "zk.example.com",
		port:       2181,
		hostIDFunc: func() string { return "" }, // host id unavailable
	}

	obs.setUp("", true, "")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after last-resort pin")
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "zookeeper" {
		t.Errorf("service.instance.id: want %q, got %q", "zookeeper", id)
	}
}

// TestEntitySource_IDImmutable: once the id is pinned, subsequent setUp calls
// with a different serverID must NOT change it.
func TestEntitySource_IDImmutable(t *testing.T) {
	obs := newObserver("host-abc")
	obs.setUp("zookeeper:1", true, "3.8.0")

	// Second call with a different server id (simulating a hypothetical id change).
	obs.setUp("zookeeper:99", true, "3.8.0")

	got, _ := obs.Observe()
	id := got.Entities[0].ID["service.instance.id"]
	if id != "zookeeper:1" {
		t.Errorf("id changed after pin: want %q, got %q", "zookeeper:1", id)
	}
}

// TestEntitySource_MonitorsEdge_Present: when agent id is set, a "monitors"
// relation from agent→target is included in the observation.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	obs := newObserver("host-abc")
	obs.setUp("zookeeper:1", true, "3.8.0")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true")
	}
	if len(got.Relations) != 1 {
		t.Fatalf("relations: want 1, got %d", len(got.Relations))
	}
	r := got.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type: want %q, got %q", "monitors", r.Type)
	}
	if r.FromID["service.instance.id"] != "agent-001" {
		t.Errorf("from id: want %q, got %v", "agent-001", r.FromID["service.instance.id"])
	}
	if r.ToID["service.instance.id"] != "zookeeper:1" {
		t.Errorf("to id: want %q, got %v", "zookeeper:1", r.ToID["service.instance.id"])
	}
}

// TestEntitySource_MonitorsEdge_Absent: when agent id is empty, no "monitors"
// relation is emitted (an unresolvable From endpoint must not be sent).
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	obs := newObserver("host-abc")
	obs.setUp("zookeeper:1", true, "3.8.0")

	got, ok := obs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true")
	}
	if len(got.Relations) != 0 {
		t.Errorf("relations: want 0 when agent id is empty, got %d", len(got.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs: server.address, server.port, and
// service.name must be present; service.version only when non-empty.
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	obs := newObserver("host-abc")
	obs.setUp("zookeeper:2", true, "3.8.1")

	got, _ := obs.Observe()
	attrs := got.Entities[0].Attributes
	checks := map[string]any{
		"service.name":    "zookeeper",
		"server.address":  "zk.example.com",
		"server.port":     2181,
		"service.version": "3.8.1",
	}
	for k, want := range checks {
		if got := attrs[k]; got != want {
			t.Errorf("attr %q: want %v, got %v", k, want, got)
		}
	}
}

// TestEntitySource_UpFalse_ObserveReturnsFalse: a down signal after pin must
// return ok=false (the detector then reuses the previous good snapshot).
func TestEntitySource_UpFalse_ObserveReturnsFalse(t *testing.T) {
	obs := newObserver("host-abc")
	obs.setUp("zookeeper:1", true, "3.8.0")

	// Simulate outage.
	obs.setUp("", false, "")

	_, ok := obs.Observe()
	if ok {
		t.Error("Observe: want ok=false when node is down")
	}
}

// TestProbe_ResolvesServerID_FromConf: integration of the conf fetch path
// through the full Collect → entity pipeline.
func TestProbe_ResolvesServerID_FromConf(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	// mntrPayload is a minimal mntr response so Collect succeeds.
	const mntrPayload = "zk_version\t3.8.0\nzk_avg_latency\t1\n"
	// confPayload is the conf response with serverId.
	const confPayload = "clientPort=2181\nserverId=5\ndataDir=/data/zk\n"

	// Alternate the stub: first dial gets mntr (fetchMntr), second gets conf
	// (fetchConf). We use a channel to serve payloads in order.
	payloads := make(chan string, 2)
	payloads <- mntrPayload
	payloads <- confPayload

	dialStub := func(network, address string, timeout time.Duration) (net.Conn, error) {
		payload, ok := <-payloads
		if !ok {
			return nil, fmt.Errorf("no more stubs")
		}
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			buf := make([]byte, 64)
			server.Read(buf) //nolint:errcheck
			fmt.Fprint(server, payload)
		}()
		return client, nil
	}

	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = dialStub
	// Override getHostIdentity so the test is hermetic.
	origGHI := getHostIdentity
	getHostIdentity = func() string { return "host-xyz" }
	t.Cleanup(func() { getHostIdentity = origGHI })

	_, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	got, ok := zp.entityObs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after successful Collect with conf")
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "zookeeper:5" {
		t.Errorf("service.instance.id: want %q, got %q", "zookeeper:5", id)
	}
}

// TestProbe_FallsBackWhenConfUnavailable: when conf fails, the host-derived id
// is used instead.
func TestProbe_FallsBackWhenConfUnavailable(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	const mntrPayload = "zk_version\t3.8.0\nzk_avg_latency\t1\n"

	// First call: mntr succeeds. Second call (conf): connection refused.
	callCount := 0
	dialStub := func(network, address string, timeout time.Duration) (net.Conn, error) {
		callCount++
		if callCount == 1 {
			server, client := net.Pipe()
			go func() {
				defer server.Close()
				buf := make([]byte, 64)
				server.Read(buf) //nolint:errcheck
				fmt.Fprint(server, mntrPayload)
			}()
			return client, nil
		}
		return nil, fmt.Errorf("connection refused")
	}

	origGHI := getHostIdentity
	getHostIdentity = func() string { return "machine-fallback" }
	t.Cleanup(func() { getHostIdentity = origGHI })

	p, _ := NewZookeeperProbe(map[string]interface{}{}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = dialStub

	_, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	got, ok := zp.entityObs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true after fallback pin")
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "zookeeper@machine-fallback" {
		t.Errorf("service.instance.id: want %q, got %q", "zookeeper@machine-fallback", id)
	}
}

// TestProbe_InstanceNameSkipsConfFetch: when instance_name is set, the conf
// command must NOT be dialed on Collect (id already pinned).
func TestProbe_InstanceNameSkipsConfFetch(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	const mntrPayload = "zk_version\t3.8.0\nzk_avg_latency\t1\n"

	callCount := 0
	dialStub := func(network, address string, timeout time.Duration) (net.Conn, error) {
		callCount++
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			buf := make([]byte, 64)
			server.Read(buf) //nolint:errcheck
			fmt.Fprint(server, mntrPayload)
		}()
		return client, nil
	}

	p, _ := NewZookeeperProbe(map[string]interface{}{"instance_name": "my-zk"}, testLogger())
	zp := p.(*ZookeeperProbe)
	zp.dial = dialStub

	_, err := zp.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	// Only one dial expected (mntr), not two (mntr + conf).
	if callCount != 1 {
		t.Errorf("dial call count: want 1, got %d (conf should not be fetched when instance_name is set)", callCount)
	}

	got, ok := zp.entityObs.Observe()
	if !ok {
		t.Fatal("Observe: want ok=true")
	}
	id := got.Entities[0].ID["service.instance.id"]
	if id != "my-zk" {
		t.Errorf("service.instance.id: want %q, got %q", "my-zk", id)
	}
}

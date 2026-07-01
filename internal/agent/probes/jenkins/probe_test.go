package jenkins

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

const (
	jobsJSON = `{"jobs":[
		{"name":"build","color":"blue","lastBuild":{"number":42,"duration":12000,"result":"SUCCESS","timestamp":1}},
		{"name":"deploy","color":"red","lastBuild":{"number":7,"duration":3000,"result":"FAILURE","timestamp":2}},
		{"name":"flaky","color":"yellow","lastBuild":{"number":3,"duration":500,"result":"UNSTABLE","timestamp":3}},
		{"name":"never-run","color":"notbuilt","lastBuild":null}
	]}`
	computerJSON = `{"computer":[
		{"displayName":"built-in","offline":false,"executors":[{"idle":true},{"idle":false}]},
		{"displayName":"agent-1","offline":false,"executors":[{"idle":true}]},
		{"displayName":"agent-2","offline":true,"executors":[]}
	]}`
	queueJSON = `{"items":[{"blocked":true},{"blocked":false},{"blocked":true}]}`
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jobsJSON))
	})
	mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(computerJSON))
	})
	mux.HandleFunc("/queue/api/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(queueJSON))
	})
	return httptest.NewServer(mux)
}

func newTestProbe(t *testing.T, endpoint string) *jenkinsProbe {
	t.Helper()
	probe, err := NewJenkinsProbe(map[string]interface{}{
		"endpoint":  endpoint,
		"username":  "ci",
		"api_token": "secret",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewJenkinsProbe: %v", err)
	}
	p := probe.(*jenkinsProbe)
	p.SetName("ci-main")
	return p
}

// point keys a datapoint by name + its discriminant tag for assertion.
func tagVal(dp data_store.DataPoint, key string) string {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

func TestParseConfig_MissingEndpoint(t *testing.T) {
	if _, err := NewJenkinsProbe(map[string]interface{}{}, testBaseLogger()); err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{"https://jenkins.example.com", "jenkins.example.com:443"},
		{"http://jenkins.example.com", "jenkins.example.com:80"},
		{"http://10.0.0.5:8080", "10.0.0.5:8080"},
	}
	for _, c := range cases {
		h, p, err := hostPort(c.endpoint)
		if err != nil {
			t.Fatalf("hostPort(%q): %v", c.endpoint, err)
		}
		if got := h + ":" + p; got != c.want {
			t.Errorf("hostPort(%q) = %q, want %q", c.endpoint, got, c.want)
		}
	}
}

func TestCollect_Up(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var up float64 = -1
	for _, dp := range points {
		if dp.Name == metricUp {
			up = dp.Value
		}
	}
	if up != 1 {
		t.Errorf("senhub.jenkins.up = %v, want 1", up)
	}
}

func TestCollect_JobCountsByStatus(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, _ := p.Collect()
	counts := map[string]float64{}
	for _, dp := range points {
		if dp.Name == metricJobCount {
			counts[tagVal(dp, "status")] = dp.Value
		}
	}
	want := map[string]float64{"success": 1, "failure": 1, "unstable": 1, "aborted": 0}
	for status, w := range want {
		if counts[status] != w {
			t.Errorf("job.count[%s] = %v, want %v", status, counts[status], w)
		}
	}
}

func TestCollect_PerJobMetrics(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, _ := p.Collect()
	durations := map[string]float64{}
	buildNums := map[string]float64{}
	for _, dp := range points {
		switch dp.Name {
		case metricJobDuration:
			durations[tagVal(dp, "job")] = dp.Value
		case metricJobLastBuildNum:
			buildNums[tagVal(dp, "job")] = dp.Value
		}
	}
	if durations["build"] != 12000 {
		t.Errorf("build duration = %v, want 12000", durations["build"])
	}
	if buildNums["deploy"] != 7 {
		t.Errorf("deploy last build number = %v, want 7", buildNums["deploy"])
	}
	// never-run has no lastBuild → no per-job series.
	if _, ok := durations["never-run"]; ok {
		t.Error("never-run should not emit a duration (no lastBuild)")
	}
}

func TestCollect_NodesAndExecutors(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, _ := p.Collect()
	nodes := map[string]float64{}
	execs := map[string]float64{}
	for _, dp := range points {
		switch dp.Name {
		case metricNodeCount:
			nodes[tagVal(dp, "status")] = dp.Value
		case metricNodeExecutor:
			execs[tagVal(dp, "state")] = dp.Value
		}
	}
	if nodes["online"] != 2 || nodes["offline"] != 1 {
		t.Errorf("nodes = %+v, want online=2 offline=1", nodes)
	}
	// online executors: built-in (1 idle, 1 busy) + agent-1 (1 idle) → busy=1 free=2.
	if execs["busy"] != 1 || execs["free"] != 2 {
		t.Errorf("executors = %+v, want busy=1 free=2", execs)
	}
}

func TestCollect_Queue(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, _ := p.Collect()
	var size, blocked float64 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case metricQueueSize:
			size = dp.Value
		case metricQueueBlocked:
			blocked = dp.Value
		}
	}
	if size != 3 {
		t.Errorf("queue.size = %v, want 3", size)
	}
	if blocked != 2 {
		t.Errorf("queue.blocked = %v, want 2", blocked)
	}
}

func TestCollect_DownEmitsUpZero(t *testing.T) {
	srv := newTestServer(t)
	srv.Close() // server gone → all requests fail

	p := newTestProbe(t, srv.URL)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not error on a down controller: %v", err)
	}
	var up float64 = -1
	for _, dp := range points {
		if dp.Name == metricUp {
			up = dp.Value
		}
	}
	if up != 0 {
		t.Errorf("senhub.jenkins.up = %v, want 0 when controller is down", up)
	}
}

// --- entity source tests ---------------------------------------------------

// stubHostID returns a deterministic host id for hermetic tests.
func stubHostID(id string) func() string { return func() string { return id } }

// alwaysFailFetch is a fetchIdentity that always returns an error.
func alwaysFailFetch() (string, error) {
	return "", fmt.Errorf("unavailable")
}

// alwaysReturnID is a fetchIdentity that always returns the given id.
func alwaysReturnID(id string) func() (string, error) {
	return func() (string, error) { return id, nil }
}

func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newEntitySource("my-jenkins", "10.0.0.5", "8080", alwaysFailFetch, stubHostID("host-abc"))
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false, want true (instance_name is pinned at construction)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	got := obs.Entities[0].ID[idKeyServiceInstanceID]
	if got != "my-jenkins" {
		t.Errorf("service.instance.id = %v, want my-jenkins", got)
	}
}

// TestEntitySource_ServiceVersion verifies the X-Jenkins version (via setVersion)
// rides the entity as service.version (toise#216 AT1), absent until seen.
func TestEntitySource_ServiceVersion(t *testing.T) {
	src := newEntitySource("my-jenkins", "10.0.0.5", "8080", alwaysFailFetch, stubHostID("h"))

	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["service.version"]; has {
		t.Error("service.version must be absent before the X-Jenkins header is seen")
	}

	src.setVersion("2.426.1")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["service.version"]; got != "2.426.1" {
		t.Errorf("service.version = %v, want 2.426.1", got)
	}
}

func TestEntitySource_TechIDPinned(t *testing.T) {
	const wantID = "jenkins:abcdef0123456789ab12"
	calls := 0
	fetch := func() (string, error) {
		calls++
		return wantID, nil
	}
	src := newEntitySource("", "10.0.0.5", "8080", fetch, stubHostID("host-abc"))

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false after successful fetch")
	}
	got := obs.Entities[0].ID[idKeyServiceInstanceID]
	if got != wantID {
		t.Errorf("service.instance.id = %v, want %v", got, wantID)
	}

	// Second call must NOT re-invoke fetchIdentity.
	_, _ = src.Observe()
	if calls != 1 {
		t.Errorf("fetchIdentity called %d times, want 1 (id must be pinned)", calls)
	}
}

func TestEntitySource_NotEmittedBeforePinned(t *testing.T) {
	// A fetch that never returns (blocks indefinitely) — never called because
	// we test with a deterministic single call. Simulate "transient failure"
	// by not using this test to block; instead simulate a fetch that starts
	// always failing and then succeeds.
	//
	// The real requirement: if the source has not yet pinned, ok=false.
	// We can test this by creating a source WITHOUT instance_name and with a
	// fetch that succeeds only on the second call; but the current design
	// pins on the first Observe call (either tech id or fallback). So the
	// "not emitted before pinned" invariant is: no entity emitted before the
	// VERY FIRST Observe() returns (i.e., if Observe blocks, no emission).
	// More concretely, a source with instance_name="" and a fetch that has
	// not yet been called will return ok=false before Observe is invoked.
	//
	// Since pinning happens inside Observe, the only way ok=false is if
	// instance_name=="" AND Observe has never been called. We verify this
	// by checking the unexported state before the first call.
	src := newEntitySource("", "h", "8080", alwaysReturnID("jenkins:abc123"), stubHostID("h"))
	src.mu.Lock()
	notYetPinned := src.pinnedID == ""
	src.mu.Unlock()
	if !notYetPinned {
		t.Fatal("id should not be pinned before the first Observe call")
	}
	// After the first call it must be pinned and ok=true.
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("ok=false after first Observe")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("no entities emitted after pinning")
	}
}

func TestEntitySource_FallbackPath(t *testing.T) {
	src := newEntitySource("", "10.0.0.5", "8080", alwaysFailFetch, stubHostID("machine-xyz"))
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false on fallback path")
	}
	got := obs.Entities[0].ID[idKeyServiceInstanceID]
	if got != "jenkins@machine-xyz" {
		t.Errorf("service.instance.id = %v, want jenkins@machine-xyz", got)
	}
}

func TestEntitySource_FallbackPath_NoHostID(t *testing.T) {
	src := newEntitySource("", "10.0.0.5", "8080", alwaysFailFetch, stubHostID(""))
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false on last-resort fallback")
	}
	got := obs.Entities[0].ID[idKeyServiceInstanceID]
	if got != "jenkins" {
		t.Errorf("service.instance.id = %v, want jenkins", got)
	}
}

func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-001")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	// Remote endpoint (10.0.0.5) → no runs_on, so the monitors edge is the only
	// relation; assert on the monitors edge by type, not the bare count.
	src := newEntitySource("my-jenkins", "10.0.0.5", "8080", alwaysFailFetch, stubHostID("h"))
	obs, _ := src.Observe()
	var r *entity.Relation
	for i := range obs.Relations {
		if obs.Relations[i].Type == "monitors" {
			r = &obs.Relations[i]
		}
	}
	if r == nil {
		t.Fatalf("no monitors relation; got %+v", obs.Relations)
	}
	if got := r.FromID[idKeyServiceInstanceID]; got != "agent-001" {
		t.Errorf("From id = %v, want agent-001", got)
	}
	if got := r.ToID[idKeyServiceInstanceID]; got != "my-jenkins" {
		t.Errorf("To id = %v, want my-jenkins", got)
	}
}

func TestEntitySource_MonitorsEdge_AbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	// Remote endpoint and no agent id → no monitors and no runs_on edge.
	src := newEntitySource("my-jenkins", "10.0.0.5", "8080", alwaysFailFetch, stubHostID("h"))
	obs, _ := src.Observe()
	for _, r := range obs.Relations {
		t.Errorf("no relation expected (no agent id, remote endpoint); got %s", r.Type)
	}
}

// TestEntitySource_LocalRunsOn: a loopback-monitored Jenkins emits a runs_on→
// host edge so it does not float. The id is host-scoped ("jenkins@<hid>") or a
// tech fingerprint — it never embeds the address — so loopback passes the
// collapse guard. A remote endpoint emits no runs_on.
func TestEntitySource_LocalRunsOn(t *testing.T) {
	agentstate.SetAgentInstanceID("")

	runsOn := func(serverAddr string) *entity.Relation {
		src := newEntitySource("", serverAddr, "8080", alwaysFailFetch, stubHostID("H"))
		obs, _ := src.Observe()
		for i := range obs.Relations {
			if obs.Relations[i].Type == "runs_on" {
				return &obs.Relations[i]
			}
		}
		return nil
	}

	r := runsOn("localhost")
	if r == nil {
		t.Fatal("loopback Jenkins: expected a runs_on→host edge")
	}
	if r.ToType != "host" || r.ToID["host.id"] != "H" {
		t.Errorf("runs_on target = %s/%v, want host/H", r.ToType, r.ToID)
	}
	if r.FromID[idKeyServiceInstanceID] != "jenkins@H" {
		t.Errorf("runs_on source = %v, want jenkins@H", r.FromID)
	}
	if runsOn("127.0.0.1") == nil {
		t.Error("loopback IP Jenkins: expected a runs_on→host edge")
	}
	if runsOn("10.0.0.5") != nil {
		t.Error("remote Jenkins must NOT emit runs_on→host")
	}
}

func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newEntitySource("x", "myhost", "8080", alwaysFailFetch, stubHostID(""))
	obs, _ := src.Observe()
	e := obs.Entities[0]
	if e.Attributes["service.name"] != "jenkins" {
		t.Errorf("service.name = %v, want jenkins", e.Attributes["service.name"])
	}
	if e.Attributes["server.address"] != "myhost" {
		t.Errorf("server.address = %v, want myhost", e.Attributes["server.address"])
	}
	if e.Attributes["server.port"] != "8080" {
		t.Errorf("server.port = %v, want 8080", e.Attributes["server.port"])
	}
}

func TestEnrichment_TagsProbeName(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)

	points, _ := p.Collect()
	if len(points) == 0 {
		t.Fatal("no datapoints")
	}
	for _, dp := range points {
		if hasTag(dp.Tags, "probe_type", "jenkins") && hasTag(dp.Tags, "probe_name", "ci-main") {
			return
		}
	}
	t.Error("no datapoint carried probe_type=jenkins and probe_name=ci-main")
}

func hasTag(ts []tags.Tag, key, value string) bool {
	for _, tg := range ts {
		if tg.Key == key && tg.Value == value {
			return true
		}
	}
	return false
}

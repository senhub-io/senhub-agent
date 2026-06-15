package jenkins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
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

	var up float32 = -1
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
	counts := map[string]float32{}
	for _, dp := range points {
		if dp.Name == metricJobCount {
			counts[tagVal(dp, "status")] = dp.Value
		}
	}
	want := map[string]float32{"success": 1, "failure": 1, "unstable": 1, "aborted": 0}
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
	durations := map[string]float32{}
	buildNums := map[string]float32{}
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
	nodes := map[string]float32{}
	execs := map[string]float32{}
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
	var size, blocked float32 = -1, -1
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
	var up float32 = -1
	for _, dp := range points {
		if dp.Name == metricUp {
			up = dp.Value
		}
	}
	if up != 0 {
		t.Errorf("senhub.jenkins.up = %v, want 0 when controller is down", up)
	}
}

func TestEntitySource_ServiceInstance(t *testing.T) {
	src := newEntitySource("10.0.0.5:8080")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe ok=false, want true")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != entityTypeServiceInstance {
		t.Errorf("entity type = %q, want %q", e.Type, entityTypeServiceInstance)
	}
	if got := e.ID[idKeyServiceInstanceID]; got != "jenkins://10.0.0.5:8080" {
		t.Errorf("service.instance.id = %v, want jenkins://10.0.0.5:8080", got)
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

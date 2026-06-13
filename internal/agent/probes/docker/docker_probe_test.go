package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// makeTestServer returns an httptest.Server that serves fake Docker Engine API
// responses. containerList is served at /v1.43/containers/json;
// statsByID maps container ID → stats blob for /v1.43/containers/{id}/stats.
// A nil stats value causes the server to return 409 (stopped container);
// a missing key causes 404 (disappeared container).
func makeTestServer(t *testing.T, containerList []containerListItem, statsByID map[string]*containerStats) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/"+apiVersion+"/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(containerList)

		case strings.HasSuffix(r.URL.Path, "/stats"):
			// Extract container id from /<api>/containers/{id}/stats
			parts := strings.Split(r.URL.Path, "/")
			// parts: ["", "v1.43", "containers", "<id>", "stats"]
			if len(parts) < 5 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			id := parts[len(parts)-2]
			s, ok := statsByID[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if s == nil {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv
}

// redirectTransport rewrites scheme+host of every outgoing request to the
// given target URL. Used to route the probe's http.Client at an httptest.Server
// rather than the Unix socket — without changing the URL-building logic.
type redirectTransport struct {
	base   http.RoundTripper
	target string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(rt.target, "http://")
	return rt.base.RoundTrip(clone)
}

// buildTestProbe constructs a dockerProbe with its http.Client wired to srv.
func buildTestProbe(t *testing.T, srv *httptest.Server) *dockerProbe {
	t.Helper()
	p, err := NewDockerProbe(map[string]interface{}{}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewDockerProbe: %v", err)
	}
	dp := p.(*dockerProbe)
	dp.client = &http.Client{
		Transport: &redirectTransport{
			base:   srv.Client().Transport,
			target: srv.URL,
		},
	}
	return dp
}

// indexByName returns a map from metric name → last DataPoint value for that
// name among the collected points.
func indexByName(points []data_store.DataPoint) map[string]float32 {
	m := make(map[string]float32)
	for _, pt := range points {
		m[pt.Name] = pt.Value
	}
	return m
}

// assertMetric fails the test when the named metric is absent or has an
// unexpected value.
func assertMetric(t *testing.T, byName map[string]float32, name string, want float32) {
	t.Helper()
	got, ok := byName[name]
	if !ok {
		t.Errorf("expected metric %s to be present", name)
		return
	}
	if got != want {
		t.Errorf("metric %s: want %v, got %v", name, want, got)
	}
}

// makeRunningStats returns a populated containerStats for a running container.
func makeRunningStats() *containerStats {
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 1_000_000_000
	s.CPUStats.CPUUsage.PercpuUsage = []uint64{500_000_000, 500_000_000}
	s.CPUStats.SystemCPUUsage = 10_000_000_000
	s.MemoryStats.Usage = 256 * 1024 * 1024
	s.MemoryStats.Limit = 1024 * 1024 * 1024
	s.Networks = map[string]struct {
		TxBytes   uint64 `json:"tx_bytes"`
		RxBytes   uint64 `json:"rx_bytes"`
		TxPackets uint64 `json:"tx_packets"`
		RxPackets uint64 `json:"rx_packets"`
	}{
		"eth0": {TxBytes: 1000, RxBytes: 2000, TxPackets: 10, RxPackets: 20},
	}
	s.BlkioStats.IOServiceBytesRecursive = []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	}{
		{Op: "Read", Value: 4096},
		{Op: "Write", Value: 8192},
		{Op: "Total", Value: 12288},
	}
	return s
}

// TestCollect_EmptyDaemon verifies that an empty container list produces zero
// datapoints without error.
func TestCollect_EmptyDaemon(t *testing.T) {
	srv := makeTestServer(t, nil, nil)
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("expected 0 datapoints for empty daemon, got %d", len(points))
	}
}

// TestCollect_OneRunningContainer verifies all metric families for a single
// running container with known CPU, memory, network, blkio and restart values.
func TestCollect_OneRunningContainer(t *testing.T) {
	c := containerListItem{
		ID:           "abc123def456789012",
		Names:        []string{"/myapp"},
		Image:        "nginx:latest",
		State:        "running",
		RestartCount: 2,
	}
	s := makeRunningStats()

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	assertMetric(t, byName, "senhub.docker.up", 1)
	assertMetric(t, byName, "container.restarts", 2)
	assertMetric(t, byName, "container.cpu.usage.total", 1_000_000_000)
	assertMetric(t, byName, "senhub.docker.cpu.system", 10_000_000_000)
	assertMetric(t, byName, "senhub.docker.cpu.online", 2)
	assertMetric(t, byName, "container.memory.usage", float32(256*1024*1024))
	assertMetric(t, byName, "senhub.docker.memory.limit", float32(1024*1024*1024))
	assertMetric(t, byName, "container.network.io.usage.tx_bytes", 1000)
	assertMetric(t, byName, "container.network.io.usage.rx_bytes", 2000)
	assertMetric(t, byName, "senhub.docker.network.tx_packets", 10)
	assertMetric(t, byName, "senhub.docker.network.rx_packets", 20)
	assertMetric(t, byName, "container.blockio.usage.total", 12288)
}

// TestCollect_StoppedContainer verifies that a stopped container (stats endpoint
// returns 409) emits senhub.docker.up=0 and container.restarts, but no resource
// metrics. The collection must succeed (err=nil).
func TestCollect_StoppedContainer(t *testing.T) {
	c := containerListItem{
		ID:           "stopped0001234567890",
		Names:        []string{"/stopped"},
		Image:        "alpine:latest",
		State:        "exited",
		RestartCount: 5,
	}
	// nil stats value → server returns 409
	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: nil})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	assertMetric(t, byName, "senhub.docker.up", 0)
	assertMetric(t, byName, "container.restarts", 5)

	// Resource metrics must be absent for a stopped container.
	for _, name := range []string{
		"container.cpu.usage.total",
		"container.memory.usage",
		"container.network.io.usage.tx_bytes",
		"container.blockio.usage.total",
	} {
		if _, ok := byName[name]; ok {
			t.Errorf("expected no %s for stopped container, but got one", name)
		}
	}
}

// TestCollect_DisappearedContainer verifies that a container that disappears
// between list and stats (404) is handled gracefully (up=0, no resource
// metrics, no error).
func TestCollect_DisappearedContainer(t *testing.T) {
	c := containerListItem{
		ID:    "gone000000012345678",
		Names: []string{"/gone"},
		Image: "alpine:latest",
		State: "running",
	}
	// No entry in statsByID → server returns 404.
	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	assertMetric(t, byName, "senhub.docker.up", 0)
}

// TestCollect_NetworkNil verifies that a container running in host-network mode
// (stats.Networks is nil/empty) does not panic and omits network metrics.
func TestCollect_NetworkNil(t *testing.T) {
	c := containerListItem{
		ID:    "hostnet00001234567",
		Names: []string{"/hostnet"},
		Image: "redis:7",
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 500_000
	// Networks is nil (zero-value map)

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	// Network metrics must be absent.
	for _, name := range []string{
		"container.network.io.usage.tx_bytes",
		"container.network.io.usage.rx_bytes",
	} {
		if _, ok := byName[name]; ok {
			t.Errorf("expected no %s for host-network container, but got one", name)
		}
	}
	// CPU metric must still be present.
	if _, ok := byName["container.cpu.usage.total"]; !ok {
		t.Error("expected container.cpu.usage.total to be present")
	}
}

// TestCollect_IncludeFilter verifies that the include glob restricts which
// containers produce metrics.
func TestCollect_IncludeFilter(t *testing.T) {
	c1 := containerListItem{ID: "aaaa000000000001", Names: []string{"/web"}, State: "running"}
	c2 := containerListItem{ID: "bbbb000000000002", Names: []string{"/db"}, State: "running"}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 1

	srv := makeTestServer(t, []containerListItem{c1, c2},
		map[string]*containerStats{c1.ID: s, c2.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	p.cfg.Include = []string{"web"}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	// "db" container metrics must be absent.
	for _, pt := range points {
		for _, tag := range pt.Tags {
			if tag.Key == "container_name" && tag.Value == "db" {
				t.Errorf("expected db container to be filtered out, found metric %s for it", pt.Name)
			}
		}
	}
	// "web" must appear.
	found := false
	for _, pt := range points {
		for _, tag := range pt.Tags {
			if tag.Key == "container_name" && tag.Value == "web" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected metrics for web container but found none")
	}
}

// TestCollect_EnrichTags verifies that probe_name and probe_type are present
// on every datapoint (EnrichDataPointsWithProbeName contract).
func TestCollect_EnrichTags(t *testing.T) {
	c := containerListItem{
		ID:    "enrich0000001234567",
		Names: []string{"/enrichtest"},
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 100

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	p.SetName("my_docker_instance")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least one datapoint")
	}

	for _, pt := range points {
		var probeName, probeType string
		for _, tag := range pt.Tags {
			switch tag.Key {
			case "probe_name":
				probeName = tag.Value
			case "probe_type":
				probeType = tag.Value
			}
		}
		if probeName != "my_docker_instance" {
			t.Errorf("datapoint %s: expected probe_name=my_docker_instance, got %q", pt.Name, probeName)
		}
		if probeType != ProbeType {
			t.Errorf("datapoint %s: expected probe_type=%s, got %q", pt.Name, ProbeType, probeType)
		}
	}
}

// TestEntitySource_Observe_BeforeUpdate verifies ok=false before the first update.
func TestEntitySource_Observe_BeforeUpdate(t *testing.T) {
	s := &dockerEntitySource{}
	_, ok := s.Observe()
	if ok {
		t.Error("expected ok=false before first update")
	}
}

// TestEntitySource_Observe_AfterUpdate verifies entity attributes populated by update.
func TestEntitySource_Observe_AfterUpdate(t *testing.T) {
	s := &dockerEntitySource{}
	s.update([]containerListItem{
		{ID: "abc123def456789012", Names: []string{"/myapp"}, Image: "nginx:latest", State: "running"},
	})
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("expected ok=true after update")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != entityTypeContainer {
		t.Errorf("entity type: want %s, got %s", entityTypeContainer, e.Type)
	}
	gotID, _ := e.ID[idKeyContainerID].(string)
	if gotID != "abc123def456789012" {
		t.Errorf("container.id: want abc123def456789012, got %q", gotID)
	}
	gotRuntime, _ := e.Attributes[attrContainerRuntime].(string)
	if gotRuntime != "docker" {
		t.Errorf("container.runtime: want docker, got %q", gotRuntime)
	}
	gotName, _ := e.Attributes[attrContainerName].(string)
	if gotName != "myapp" {
		t.Errorf("container.name: want myapp, got %q", gotName)
	}
}

// TestEntitySource_NameStrip verifies that the leading '/' is stripped from
// Docker container names stored in entity attributes.
func TestEntitySource_NameStrip(t *testing.T) {
	s := &dockerEntitySource{}
	s.update([]containerListItem{
		{ID: "slashtest000001234", Names: []string{"/slashtest"}, Image: "alpine"},
	})
	obs, _ := s.Observe()
	if len(obs.Entities) == 0 {
		t.Fatal("no entities after update")
	}
	name, _ := obs.Entities[0].Attributes[attrContainerName].(string)
	if strings.HasPrefix(name, "/") {
		t.Errorf("container.name should not have a leading slash, got %q", name)
	}
}

// TestBlkioTotal_TotalPresent verifies that the "Total" entry is preferred
// when present.
func TestBlkioTotal_TotalPresent(t *testing.T) {
	s := &containerStats{}
	s.BlkioStats.IOServiceBytesRecursive = []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	}{
		{Op: "Read", Value: 1000},
		{Op: "Write", Value: 2000},
		{Op: "Total", Value: 5000},
	}
	if got := blkioTotal(s); got != 5000 {
		t.Errorf("blkioTotal: want 5000, got %d", got)
	}
}

// TestBlkioTotal_FallbackReadWrite verifies that Read+Write are summed when
// "Total" is absent (cgroupsv2 kernel).
func TestBlkioTotal_FallbackReadWrite(t *testing.T) {
	s := &containerStats{}
	s.BlkioStats.IOServiceBytesRecursive = []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	}{
		{Op: "Read", Value: 4096},
		{Op: "Write", Value: 8192},
	}
	if got := blkioTotal(s); got != 12288 {
		t.Errorf("blkioTotal: want 12288, got %d", got)
	}
}

// TestPrimaryName_LeadingSlash verifies the leading '/' removal.
func TestPrimaryName_LeadingSlash(t *testing.T) {
	c := containerListItem{ID: "x", Names: []string{"/foo"}}
	if got := primaryName(c); got != "foo" {
		t.Errorf("primaryName: want foo, got %q", got)
	}
}

// TestPrimaryName_NoNames falls back to the short container ID when Names is
// empty (should not happen in practice, but guard against it).
func TestPrimaryName_NoNames(t *testing.T) {
	c := containerListItem{ID: "abcdef123456789012"}
	got := primaryName(c)
	if got != "abcdef123456" {
		t.Errorf("primaryName (no names): want abcdef123456, got %q", got)
	}
}

// TestApplyFilter_Include verifies that the include glob retains only matching
// containers.
func TestApplyFilter_Include(t *testing.T) {
	p := &dockerProbe{cfg: probeConfig{Include: []string{"web*"}}}
	containers := []containerListItem{
		{ID: "1", Names: []string{"/web1"}},
		{ID: "2", Names: []string{"/db1"}},
		{ID: "3", Names: []string{"/web2"}},
	}
	got := p.applyFilter(containers)
	if len(got) != 2 {
		t.Errorf("expected 2 containers after include filter, got %d", len(got))
	}
}

// TestApplyFilter_Exclude verifies that matching containers are removed.
func TestApplyFilter_Exclude(t *testing.T) {
	p := &dockerProbe{cfg: probeConfig{Exclude: []string{"tmp*"}}}
	containers := []containerListItem{
		{ID: "1", Names: []string{"/web"}},
		{ID: "2", Names: []string{"/tmp_job"}},
	}
	got := p.applyFilter(containers)
	if len(got) != 1 {
		t.Fatalf("expected 1 container after exclude filter, got %d", len(got))
	}
	if primaryName(got[0]) != "web" {
		t.Errorf("expected web, got %q", primaryName(got[0]))
	}
}

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
// Uses cgroupsv1 memory keys (rss/cache/swap) plus the full set of deep memory
// stats exposed by the OTel docker stats receiver contrib.
func makeRunningStats() *containerStats {
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 1_000_000_000
	s.CPUStats.CPUUsage.UsageInKernelmode = 200_000_000
	s.CPUStats.CPUUsage.UsageInUsermode = 800_000_000
	s.CPUStats.CPUUsage.PercpuUsage = []uint64{500_000_000, 500_000_000}
	s.CPUStats.SystemCPUUsage = 10_000_000_000
	s.CPUStats.OnlineCPUs = 2
	s.CPUStats.ThrottlingData.ThrottledPeriods = 3
	s.CPUStats.ThrottlingData.ThrottlingPeriods = 100
	s.CPUStats.ThrottlingData.ThrottledTime = 5_000_000
	s.PreCPUStats.CPUUsage.TotalUsage = 900_000_000
	s.PreCPUStats.SystemCPUUsage = 9_000_000_000
	s.MemoryStats.Usage = 256 * 1024 * 1024
	s.MemoryStats.Limit = 1024 * 1024 * 1024
	s.MemoryStats.Stats = map[string]uint64{
		"rss":                        200 * 1024 * 1024,
		"cache":                      56 * 1024 * 1024,
		"swap":                       0,
		"mapped_file":                8 * 1024 * 1024,
		"pgfault":                    1234,
		"pgmajfault":                 5,
		"unevictable":                0,
		"writeback":                  512 * 1024,
		"hierarchical_memory_limit":  1024 * 1024 * 1024,
		"active_anon":                150 * 1024 * 1024,
		"inactive_anon":              50 * 1024 * 1024,
		"active_file":                30 * 1024 * 1024,
		"inactive_file":              26 * 1024 * 1024,
	}
	s.Networks = map[string]struct {
		TxBytes   uint64 `json:"tx_bytes"`
		RxBytes   uint64 `json:"rx_bytes"`
		TxPackets uint64 `json:"tx_packets"`
		RxPackets uint64 `json:"rx_packets"`
		TxErrors  uint64 `json:"tx_errors"`
		RxErrors  uint64 `json:"rx_errors"`
		TxDropped uint64 `json:"tx_dropped"`
		RxDropped uint64 `json:"rx_dropped"`
	}{
		"eth0": {TxBytes: 1000, RxBytes: 2000, TxPackets: 10, RxPackets: 20, TxErrors: 1, RxErrors: 2, TxDropped: 3, RxDropped: 4},
	}
	s.BlkioStats.IOServiceBytesRecursive = []blkioEntry{
		{Op: "Read", Value: 4096},
		{Op: "Write", Value: 8192},
		{Op: "Total", Value: 12288},
	}
	s.BlkioStats.IOServiceTimeRecursive = []blkioEntry{
		{Op: "Read", Value: 1000000},
		{Op: "Write", Value: 2000000},
		{Op: "Total", Value: 3000000},
	}
	s.BlkioStats.IOSectorsRecursive = []blkioEntry{
		{Op: "Read", Value: 8},
		{Op: "Write", Value: 16},
		{Op: "Total", Value: 24},
	}
	s.PidsStats.Current = 5
	s.PidsStats.Limit = 100
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

	// CPU base metrics
	assertMetric(t, byName, "container.cpu.usage.total", 1_000_000_000)
	assertMetric(t, byName, "container.cpu.usage.kernelmode", 200_000_000)
	assertMetric(t, byName, "container.cpu.usage.usermode", 800_000_000)
	assertMetric(t, byName, "senhub.docker.cpu.system", 10_000_000_000)
	assertMetric(t, byName, "senhub.docker.cpu.online", 2)
	// cpu.percent: cpuDelta=100M, systemDelta=1B, 2 CPUs → (100M/1B)*2*100 = 20.0
	assertMetric(t, byName, "senhub.docker.cpu.percent", 20.0)

	// CPU throttling
	assertMetric(t, byName, "container.cpu.throttling_data.throttled_periods", 3)
	assertMetric(t, byName, "container.cpu.throttling_data.periods", 100)
	assertMetric(t, byName, "container.cpu.throttling_data.throttled_time", 5_000_000)

	// Per-core CPU: indexByName keeps the last value per name — verify the metric exists
	// by checking any occurrence in the full points slice.
	foundCore0, foundCore1 := false, false
	for _, pt := range points {
		if pt.Name != "container.cpu.usage.percpu" {
			continue
		}
		for _, tag := range pt.Tags {
			if tag.Key == "core" && tag.Value == "0" {
				foundCore0 = true
				if pt.Value != 500_000_000 {
					t.Errorf("container.cpu.usage.percpu core=0: want 500000000, got %v", pt.Value)
				}
			}
			if tag.Key == "core" && tag.Value == "1" {
				foundCore1 = true
				if pt.Value != 500_000_000 {
					t.Errorf("container.cpu.usage.percpu core=1: want 500000000, got %v", pt.Value)
				}
			}
		}
	}
	if !foundCore0 {
		t.Error("expected container.cpu.usage.percpu for core=0")
	}
	if !foundCore1 {
		t.Error("expected container.cpu.usage.percpu for core=1")
	}

	// Memory (cgroupsv1 base)
	assertMetric(t, byName, "container.memory.usage", float32(256*1024*1024))
	assertMetric(t, byName, "senhub.docker.memory.limit", float32(1024*1024*1024))
	assertMetric(t, byName, "container.memory.rss", float32(200*1024*1024))
	assertMetric(t, byName, "container.memory.cache", float32(56*1024*1024))
	assertMetric(t, byName, "container.memory.swap", 0)
	assertMetric(t, byName, "senhub.docker.memory.working_set", float32(200*1024*1024)) // 256-56

	// Deep memory stats (cgroupsv1 fields present in makeRunningStats)
	assertMetric(t, byName, "container.memory.anon", float32(200*1024*1024)) // fallback to rss on v1
	assertMetric(t, byName, "container.memory.mapped_file", float32(8*1024*1024))
	assertMetric(t, byName, "container.memory.pgfault", 1234)
	assertMetric(t, byName, "container.memory.pgmajfault", 5)
	assertMetric(t, byName, "container.memory.unevictable", 0)
	assertMetric(t, byName, "container.memory.writeback", float32(512*1024))
	assertMetric(t, byName, "container.memory.hierarchical_memory_limit", float32(1024*1024*1024))
	assertMetric(t, byName, "container.memory.active_anon", float32(150*1024*1024))
	assertMetric(t, byName, "container.memory.inactive_anon", float32(50*1024*1024))
	assertMetric(t, byName, "container.memory.active_file", float32(30*1024*1024))
	assertMetric(t, byName, "container.memory.inactive_file", float32(26*1024*1024))

	// PIDs
	assertMetric(t, byName, "container.pids.count", 5)
	assertMetric(t, byName, "senhub.docker.pids.limit", 100)

	// Network
	assertMetric(t, byName, "container.network.io.usage.tx_bytes", 1000)
	assertMetric(t, byName, "container.network.io.usage.rx_bytes", 2000)
	assertMetric(t, byName, "senhub.docker.network.tx_packets", 10)
	assertMetric(t, byName, "senhub.docker.network.rx_packets", 20)
	assertMetric(t, byName, "container.network.io.usage.tx_errors", 1)
	assertMetric(t, byName, "container.network.io.usage.rx_errors", 2)
	assertMetric(t, byName, "senhub.docker.network.tx_dropped", 3)
	assertMetric(t, byName, "senhub.docker.network.rx_dropped", 4)

	// Block I/O bytes
	assertMetric(t, byName, "container.blockio.usage.total", 12288)
	assertMetric(t, byName, "container.blockio.io_service_bytes_recursive.read", 4096)
	assertMetric(t, byName, "container.blockio.io_service_bytes_recursive.write", 8192)

	// Block I/O service time and sectors (cgroupsv1; present in makeRunningStats)
	assertMetric(t, byName, "senhub.docker.blkio.service_time.total", 3000000)
	assertMetric(t, byName, "senhub.docker.blkio.sectors.total", 24)
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

// TestBlkioSplit_TotalPresent verifies that the "Total" entry is preferred
// when present, and that read/write are reported separately.
func TestBlkioSplit_TotalPresent(t *testing.T) {
	entries := []blkioEntry{
		{Op: "Read", Value: 1000},
		{Op: "Write", Value: 2000},
		{Op: "Total", Value: 5000},
	}
	total, read, write := blkioSplit(entries)
	if total != 5000 {
		t.Errorf("blkioSplit total: want 5000, got %d", total)
	}
	if read != 1000 {
		t.Errorf("blkioSplit read: want 1000, got %d", read)
	}
	if write != 2000 {
		t.Errorf("blkioSplit write: want 2000, got %d", write)
	}
}

// TestBlkioSplit_FallbackReadWrite verifies that total=Read+Write when
// "Total" is absent (cgroupsv2 kernel).
func TestBlkioSplit_FallbackReadWrite(t *testing.T) {
	entries := []blkioEntry{
		{Op: "Read", Value: 4096},
		{Op: "Write", Value: 8192},
	}
	total, read, write := blkioSplit(entries)
	if total != 12288 {
		t.Errorf("blkioSplit total: want 12288, got %d", total)
	}
	if read != 4096 {
		t.Errorf("blkioSplit read: want 4096, got %d", read)
	}
	if write != 8192 {
		t.Errorf("blkioSplit write: want 8192, got %d", write)
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

// TestCollect_CgroupsV2Memory verifies that cgroupsv2 memory keys (anon/file/inactive_file)
// are used when the "rss" key is absent from memory_stats.stats.
func TestCollect_CgroupsV2Memory(t *testing.T) {
	c := containerListItem{
		ID:    "cgv2000000001234567",
		Names: []string{"/cgv2test"},
		Image: "alpine:latest",
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 100
	s.CPUStats.OnlineCPUs = 1
	s.MemoryStats.Usage = 100 * 1024 * 1024
	s.MemoryStats.Stats = map[string]uint64{
		"anon":          60 * 1024 * 1024,
		"file":          30 * 1024 * 1024,
		"inactive_file": 20 * 1024 * 1024,
		"swap":          4 * 1024 * 1024,
	}

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	assertMetric(t, byName, "container.memory.rss", float32(60*1024*1024))   // anon
	assertMetric(t, byName, "container.memory.cache", float32(30*1024*1024)) // file
	assertMetric(t, byName, "container.memory.swap", float32(4*1024*1024))
	// working_set = usage - inactive_file = 100M - 20M = 80M
	assertMetric(t, byName, "senhub.docker.memory.working_set", float32(80*1024*1024))
}

// TestCollect_CPUPercentZeroSystemDelta verifies that senhub.docker.cpu.percent
// is absent (not emitted) when the system delta is zero — guards against division
// by zero on the first collection or identical snapshots.
func TestCollect_CPUPercentZeroSystemDelta(t *testing.T) {
	c := containerListItem{
		ID:    "cpuzero0000001234567",
		Names: []string{"/cpuzero"},
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 500_000_000
	s.CPUStats.SystemCPUUsage = 1_000_000_000
	s.CPUStats.OnlineCPUs = 2
	// PreCPUStats identical → systemDelta = 0
	s.PreCPUStats.CPUUsage.TotalUsage = 500_000_000
	s.PreCPUStats.SystemCPUUsage = 1_000_000_000

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	if _, ok := byName["senhub.docker.cpu.percent"]; ok {
		t.Error("expected senhub.docker.cpu.percent to be absent when systemDelta=0")
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

// TestCollect_CPUThrottling verifies that CPU throttling metrics are emitted
// and that zero values are still emitted (not gated on presence).
func TestCollect_CPUThrottling(t *testing.T) {
	c := containerListItem{
		ID:    "throttle000001234567",
		Names: []string{"/throttled"},
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 1_000_000
	s.CPUStats.OnlineCPUs = 1
	s.CPUStats.ThrottlingData.ThrottledPeriods = 10
	s.CPUStats.ThrottlingData.ThrottlingPeriods = 200
	s.CPUStats.ThrottlingData.ThrottledTime = 50_000_000

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	assertMetric(t, byName, "container.cpu.throttling_data.throttled_periods", 10)
	assertMetric(t, byName, "container.cpu.throttling_data.periods", 200)
	assertMetric(t, byName, "container.cpu.throttling_data.throttled_time", 50_000_000)
}

// TestCollect_PercpuUsage verifies that per-core datapoints are emitted with
// the correct "core" tag and value.
func TestCollect_PercpuUsage(t *testing.T) {
	c := containerListItem{
		ID:    "percpu00000001234567",
		Names: []string{"/percputest"},
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 900_000
	s.CPUStats.CPUUsage.PercpuUsage = []uint64{300_000, 400_000, 200_000}
	s.CPUStats.OnlineCPUs = 3

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	type coreVal struct {
		core  string
		value float32
	}
	var found []coreVal
	for _, pt := range points {
		if pt.Name != "container.cpu.usage.percpu" {
			continue
		}
		for _, tag := range pt.Tags {
			if tag.Key == "core" {
				found = append(found, coreVal{core: tag.Value, value: pt.Value})
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("expected 3 per-core datapoints, got %d", len(found))
	}
	// Order is deterministic (slice iteration).
	want := []coreVal{{"0", 300_000}, {"1", 400_000}, {"2", 200_000}}
	for i, w := range want {
		if found[i].core != w.core || found[i].value != w.value {
			t.Errorf("percpu[%d]: want core=%s val=%v, got core=%s val=%v", i, w.core, w.value, found[i].core, found[i].value)
		}
	}
}

// TestCollect_BlkioServiceTimeSectors verifies that io_service_time and
// io_sectors produce datapoints when non-empty, and are absent when empty
// (cgroupsv2 behaviour).
func TestCollect_BlkioServiceTimeSectors(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		c := containerListItem{ID: "blktime0000001234567", Names: []string{"/blktime"}, State: "running"}
		s := &containerStats{}
		s.CPUStats.CPUUsage.TotalUsage = 1
		s.BlkioStats.IOServiceBytesRecursive = []blkioEntry{{Op: "Total", Value: 1024}}
		s.BlkioStats.IOServiceTimeRecursive = []blkioEntry{{Op: "Total", Value: 9_000_000}}
		s.BlkioStats.IOSectorsRecursive = []blkioEntry{{Op: "Total", Value: 32}}

		srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
		defer srv.Close()

		p := buildTestProbe(t, srv)
		points, _ := p.Collect()
		byName := indexByName(points)
		assertMetric(t, byName, "senhub.docker.blkio.service_time.total", 9_000_000)
		assertMetric(t, byName, "senhub.docker.blkio.sectors.total", 32)
	})

	t.Run("absent_on_cgroupsv2", func(t *testing.T) {
		c := containerListItem{ID: "blkv200000001234567", Names: []string{"/blkv2"}, State: "running"}
		s := &containerStats{}
		s.CPUStats.CPUUsage.TotalUsage = 1
		// IOServiceTimeRecursive and IOSectorsRecursive are nil (cgroupsv2).

		srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
		defer srv.Close()

		p := buildTestProbe(t, srv)
		points, _ := p.Collect()
		byName := indexByName(points)
		if _, ok := byName["senhub.docker.blkio.service_time.total"]; ok {
			t.Error("expected senhub.docker.blkio.service_time.total to be absent on cgroupsv2")
		}
		if _, ok := byName["senhub.docker.blkio.sectors.total"]; ok {
			t.Error("expected senhub.docker.blkio.sectors.total to be absent on cgroupsv2")
		}
	})
}

// TestCollect_DeepMemoryStats_CgroupsV2 verifies that deep memory fields are
// emitted from cgroupsv2 stats when present, and that container.memory.anon
// uses the "anon" key directly (not the "rss" fallback).
func TestCollect_DeepMemoryStats_CgroupsV2(t *testing.T) {
	c := containerListItem{
		ID:    "deepmemv2000001234567",
		Names: []string{"/deepmemv2"},
		State: "running",
	}
	s := &containerStats{}
	s.CPUStats.CPUUsage.TotalUsage = 100
	s.CPUStats.OnlineCPUs = 1
	s.MemoryStats.Usage = 200 * 1024 * 1024
	s.MemoryStats.Stats = map[string]uint64{
		"anon":          80 * 1024 * 1024,
		"file":          60 * 1024 * 1024,
		"inactive_file": 40 * 1024 * 1024,
		"swap":          0,
		"pgfault":       500,
		"pgmajfault":    2,
		"unevictable":   0,
		"writeback":     0,
		"active_anon":   70 * 1024 * 1024,
		"inactive_anon": 10 * 1024 * 1024,
		"active_file":   20 * 1024 * 1024,
	}

	srv := makeTestServer(t, []containerListItem{c}, map[string]*containerStats{c.ID: s})
	defer srv.Close()

	p := buildTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: unexpected error: %v", err)
	}

	byName := indexByName(points)
	// anon key present → used directly.
	assertMetric(t, byName, "container.memory.anon", float32(80*1024*1024))
	assertMetric(t, byName, "container.memory.pgfault", 500)
	assertMetric(t, byName, "container.memory.pgmajfault", 2)
	assertMetric(t, byName, "container.memory.unevictable", 0)
	assertMetric(t, byName, "container.memory.writeback", 0)
	assertMetric(t, byName, "container.memory.active_anon", float32(70*1024*1024))
	assertMetric(t, byName, "container.memory.inactive_anon", float32(10*1024*1024))
	assertMetric(t, byName, "container.memory.active_file", float32(20*1024*1024))
	assertMetric(t, byName, "container.memory.inactive_file", float32(40*1024*1024))
	// hierarchical_memory_limit absent from stats → metric must not be emitted.
	if _, ok := byName["container.memory.hierarchical_memory_limit"]; ok {
		t.Error("container.memory.hierarchical_memory_limit should be absent when not in stats map")
	}
}

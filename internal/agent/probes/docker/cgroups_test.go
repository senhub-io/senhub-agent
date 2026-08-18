package docker

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixture values are a verbatim capture from a cgroup v2 host (Ubuntu
// 26.04, systemd driver) running a real container, not numbers invented to
// match the parser. A reader tested against its own idea of the format proves
// nothing about the format.
const (
	fixtureID    = "01ac9bf0550f83df01d3cb0c90cdf8fb482274024c7ab71a4c5ea7d0021c06db"
	fixtureCPU   = "usage_usec 4157523025\nuser_usec 2841000000\nsystem_usec 1316523025\nnr_periods 0\nnr_throttled 0\nthrottled_usec 0\n"
	fixtureIO    = "8:0 rbytes=0 wbytes=1520070656 rios=0 wios=592840 dbytes=0 dios=0\n"
	fixtureMemSt = "anon 41893888\nfile 44539904\nkernel 6234112\ninactive_anon 0\nactive_anon 41881600\ninactive_file 27824128\nactive_file 16715776\npgfault 1256742\npgmajfault 133\n"
)

// writeCgroupFixture builds a cgroup v2 tree with one container under the
// systemd driver layout and returns the root.
func writeCgroupFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// cgroup.controllers is what marks a unified hierarchy.
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu memory io pids\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "system.slice", "docker-"+fixtureID+".scope")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"cpu.stat":       fixtureCPU,
		"io.stat":        fixtureIO,
		"memory.stat":    fixtureMemSt,
		"memory.current": "86433792\n",
		"memory.max":     "max\n",
		"pids.current":   "44\n",
		"pids.max":       "max\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCgroupUnifiedDetection(t *testing.T) {
	root := writeCgroupFixture(t)
	if !newCgroupReader(root).unifiedAvailable() {
		t.Error("a tree with cgroup.controllers must be detected as unified")
	}

	// A v1 tree has no cgroup.controllers. Reading it with the v2 parser would
	// produce partial numbers, which is worse than none — so it must not claim
	// to be available.
	v1 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(v1, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if newCgroupReader(v1).unifiedAvailable() {
		t.Error("a cgroup v1 tree must not be treated as unified")
	}
}

func TestCgroupDiscoverBothDrivers(t *testing.T) {
	root := writeCgroupFixture(t)

	// The cgroupfs driver puts containers under docker/<id> instead. Both are
	// in the field, so both must be found.
	other := "b" + fixtureID[1:]
	if err := os.MkdirAll(filepath.Join(root, "docker", other), 0o755); err != nil {
		t.Fatal(err)
	}
	// Decoys: neither is a container id.
	for _, junk := range []string{"docker/not-a-container", "system.slice/docker-short.scope"} {
		if err := os.MkdirAll(filepath.Join(root, junk), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	found, err := newCgroupReader(root).discover()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d containers, want 2 (one per driver layout): %v", len(found), found)
	}
	for _, id := range []string{fixtureID, other} {
		if _, ok := found[id]; !ok {
			t.Errorf("container %s not discovered", id)
		}
	}
}

func TestCgroupStatsMatchTheCapturedValues(t *testing.T) {
	root := writeCgroupFixture(t)
	found, err := newCgroupReader(root).discover()
	if err != nil {
		t.Fatal(err)
	}
	s, err := newCgroupReader(root).stats(found[fixtureID])
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	// cgroup reports microseconds, the Docker API nanoseconds. buildDatapoints
	// publishes the API's unit, so the conversion has to happen here or the two
	// sources would disagree by a factor of 1000 under the same metric name.
	if got, want := s.CPUStats.CPUUsage.TotalUsage, uint64(4157523025*1000); got != want {
		t.Errorf("cpu total = %d, want %d (usec converted to nsec)", got, want)
	}
	if got, want := s.CPUStats.CPUUsage.UsageInUsermode, uint64(2841000000*1000); got != want {
		t.Errorf("cpu usermode = %d, want %d", got, want)
	}

	if got, want := s.MemoryStats.Usage, uint64(86433792); got != want {
		t.Errorf("memory usage = %d, want %d", got, want)
	}
	if got, want := s.MemoryStats.Stats["anon"], uint64(41893888); got != want {
		t.Errorf("memory anon = %d, want %d", got, want)
	}
	if got, want := s.PidsStats.Current, uint64(44); got != want {
		t.Errorf("pids = %d, want %d", got, want)
	}

	var read, write uint64
	for _, e := range s.BlkioStats.IOServiceBytesRecursive {
		switch e.Op {
		case "read":
			read = e.Value
		case "write":
			write = e.Value
		}
	}
	if write != 1520070656 || read != 0 {
		t.Errorf("blkio read/write = %d/%d, want 0/1520070656", read, write)
	}
}

// "max" means unlimited. Publishing it as 0 would read as "limited to nothing",
// which is the opposite of the truth, so the field is left unset instead.
func TestCgroupUnlimitedIsNotZero(t *testing.T) {
	root := writeCgroupFixture(t)
	found, _ := newCgroupReader(root).discover()
	s, err := newCgroupReader(root).stats(found[fixtureID])
	if err != nil {
		t.Fatal(err)
	}
	if s.MemoryStats.Limit != 0 || s.PidsStats.Limit != 0 {
		t.Errorf("unlimited memory.max/pids.max must leave the field unset, got %d/%d",
			s.MemoryStats.Limit, s.PidsStats.Limit)
	}
}

// A kernel that adds an unfamiliar line to cpu.stat must not cost us every
// other counter in the file.
func TestCgroupToleratesUnknownKeys(t *testing.T) {
	root := writeCgroupFixture(t)
	dir := filepath.Join(root, "system.slice", "docker-"+fixtureID+".scope")
	if err := os.WriteFile(filepath.Join(dir, "cpu.stat"),
		[]byte(fixtureCPU+"some_future_field with three fields\nburst_usec 12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := newCgroupReader(root).stats(dir)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if s.CPUStats.CPUUsage.TotalUsage == 0 {
		t.Error("an unparseable line must not discard the rest of cpu.stat")
	}
}

func TestCgroupMissingCPUStatIsAnError(t *testing.T) {
	root := writeCgroupFixture(t)
	dir := filepath.Join(root, "system.slice", "docker-"+fixtureID+".scope")
	if err := os.Remove(filepath.Join(dir, "cpu.stat")); err != nil {
		t.Fatal(err)
	}
	if _, err := newCgroupReader(root).stats(dir); err == nil {
		t.Error("a cgroup with no cpu.stat is not readable and must report so, not return zeroes")
	}
}

// TestCollectFallsBackToCgroupsWhenTheSocketIsUnreachable is the whole point of
// the change: on a hardened non-root install the socket is denied, and the
// probe must still report resource metrics instead of nothing.
func TestCollectFallsBackToCgroupsWhenTheSocketIsUnreachable(t *testing.T) {
	root := writeCgroupFixture(t)

	p := newProbeForFallbackTest(t)
	p.cgroups = newCgroupReader(root)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("an unreachable socket with a readable cgroup tree must not be a collection error: %v", err)
	}

	if got := sourceValue(t, points, "cgroup"); got != 1 {
		t.Errorf("source[cgroup] = %v, want 1", got)
	}
	if got := sourceValue(t, points, "socket"); got != 0 {
		t.Errorf("source[socket] = %v, want 0", got)
	}

	want := map[string]bool{
		"container.cpu.usage.total": false,
		"container.memory.usage":    false,
		"container.pids.count":      false,
	}
	sawNetwork := false
	for _, pt := range points {
		if _, ok := want[pt.Name]; ok {
			want[pt.Name] = true
		}
		if pt.Name == "container.network.io.usage.rx_bytes" {
			sawNetwork = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s missing from the cgroup fallback", name)
		}
	}
	// Network counters live in the container's network namespace, not its
	// cgroup. Inventing them would be worse than their absence.
	if sawNetwork {
		t.Error("per-container network metrics cannot come from cgroups and must not be published by the fallback")
	}
}

// A host with no container cgroups is a host with no containers, not a broken
// probe: the cycle is empty and says so via the source series.
func TestCollectCgroupFallbackWithNoContainers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newProbeForFallbackTest(t)
	p.cgroups = newCgroupReader(root)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sourceValue(t, points, "cgroup"); got != 1 {
		t.Errorf("source[cgroup] = %v, want 1", got)
	}
	for _, pt := range points {
		if pt.Name != "senhub.docker.source" {
			t.Errorf("no containers, yet %q was published", pt.Name)
		}
	}
}

// cgroup v1 needs a different reader. Publishing half of it would be worse than
// publishing none, so the fallback declines and the socket error stands.
func TestCollectDoesNotFallBackOnCgroupV1(t *testing.T) {
	v1 := t.TempDir() // no cgroup.controllers
	p := newProbeForFallbackTest(t)
	p.cgroups = newCgroupReader(v1)

	if _, err := p.Collect(); err == nil {
		t.Error("on cgroup v1 the unreachable socket must surface as an error, not a silent empty cycle")
	}
}

// newProbeForFallbackTest builds a probe whose socket path points at nothing,
// so the Docker API dial fails exactly as it does for a non-root daemon facing
// a root:docker socket.
func newProbeForFallbackTest(t *testing.T) *dockerProbe {
	t.Helper()
	p, err := NewDockerProbe(map[string]interface{}{
		"socket_path": filepath.Join(t.TempDir(), "absent.sock"),
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewDockerProbe: %v", err)
	}
	return p.(*dockerProbe)
}

// TestCollectCgroupFallbackEnrichesIdentityTags is the regression guard for the
// defect the 0.5.4 field validation surfaced: the fallback returned its points
// straight out of Collect, skipping the enrichment the socket path applies, so
// every datapoint arrived at the data store with no probe_name and no
// probe_type. The transformer registry then had nothing to resolve a definition
// by, fell back to definitions/unknown.yaml, and the whole rail produced
// nothing — on a host with seven running containers, and only on the install
// shape the fallback exists to serve.
func TestCollectCgroupFallbackEnrichesIdentityTags(t *testing.T) {
	root := writeCgroupFixture(t)
	p := newProbeForFallbackTest(t)
	p.SetName("docker-preprod")
	p.cgroups = newCgroupReader(root)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("the fixture has a container: the fallback must publish something")
	}
	for _, pt := range points {
		var name, typ string
		for _, tag := range pt.Tags {
			switch tag.Key {
			case "probe_name":
				name = tag.Value
			case "probe_type":
				typ = tag.Value
			}
		}
		if name != "docker-preprod" {
			t.Errorf("datapoint %q: probe_name = %q, want docker-preprod", pt.Name, name)
		}
		if typ != ProbeType {
			t.Errorf("datapoint %q: probe_type = %q, want %q", pt.Name, typ, ProbeType)
		}
	}
}

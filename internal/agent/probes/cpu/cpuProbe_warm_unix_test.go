//go:build !windows

package cpu

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

var (
	aggT0 = cpu.TimesStat{User: 100, System: 50, Idle: 850}
	aggT1 = cpu.TimesStat{User: 130, System: 60, Idle: 910}

	coresT0 = []cpu.TimesStat{
		{CPU: "cpu0", User: 50, System: 25, Idle: 425},
		{CPU: "cpu1", User: 50, System: 25, Idle: 425},
	}
	coresT1 = []cpu.TimesStat{
		{CPU: "cpu0", User: 65, System: 30, Idle: 455},
		{CPU: "cpu1", User: 65, System: 30, Idle: 455},
	}

	kernelT0 = kernelCounters{Interrupts: 1000, ContextSwitches: 2000, ProcsRunning: 3}
	kernelT1 = kernelCounters{Interrupts: 1300, ContextSwitches: 2600, ProcsRunning: 4}
)

func newCollector(t *testing.T) *unixCollector {
	t.Helper()
	return &unixCollector{logger: logger.NewLogger(&cliArgs.ParsedArgs{})}
}

func fakeReaders(u *unixCollector, agg cpu.TimesStat, cores []cpu.TimesStat, k kernelCounters) {
	u.times = func(perCPU bool) ([]cpu.TimesStat, error) {
		if perCPU {
			return cores, nil
		}
		return []cpu.TimesStat{agg}, nil
	}
	u.kernel = func() (*kernelCounters, error) {
		kk := k
		return &kk, nil
	}
}

func collectAll(u *unixCollector, ts time.Time) []data_store.DataPoint {
	var pts []data_store.DataPoint
	var baseTags []tags.Tag
	u.collectCPUTimes(&pts, ts, baseTags)
	u.collectPerCoreMetrics(&pts, ts, baseTags)
	u.collectKernelCounters(&pts, ts, baseTags)
	return pts
}

func byName(points []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, p := range points {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

func withBoot(t *testing.T, id string) {
	t.Helper()
	prev := hostpoll.BootIdentity
	hostpoll.BootIdentity = func() (string, error) { return id, nil }
	t.Cleanup(func() { hostpoll.BootIdentity = prev })
}

func warmPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "cpu-state.json")
}

func assertNoRates(t *testing.T, pts []data_store.DataPoint, names ...string) {
	t.Helper()
	for _, name := range names {
		if got := byName(pts, name); len(got) != 0 {
			t.Errorf("%s: got %d points, want 0", name, len(got))
		}
	}
}

func assertRate(t *testing.T, pts []data_store.DataPoint, name string, want float64) {
	t.Helper()
	got := byName(pts, name)
	if len(got) != 1 {
		t.Fatalf("%s: got %d points, want 1", name, len(got))
	}
	if got[0].Value != want {
		t.Errorf("%s = %v, want %v", name, got[0].Value, want)
	}
}

func saveState(t *testing.T, path string, ts time.Time) {
	t.Helper()
	a := newCollector(t)
	a.SetState(path, time.Minute)
	fakeReaders(a, aggT0, coresT0, kernelT0)
	collectAll(a, ts)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWarmRestartEmitsOnFirstCollection(t *testing.T) {
	withBoot(t, "boot-1")
	path := warmPath(t)
	t0 := time.Unix(1000, 0)
	saveState(t, path, t0)

	b := newCollector(t)
	b.SetState(path, time.Minute)
	fakeReaders(b, aggT1, coresT1, kernelT1)
	pts := collectAll(b, t0.Add(30*time.Second))

	assertRate(t, pts, "cpu_usage_total", 40)
	assertRate(t, pts, "cpu_user", 30)
	assertRate(t, pts, "cpu_system", 10)
	assertRate(t, pts, "cpu_idle", 60)
	cores := byName(pts, "cpu_core_usage")
	if len(cores) != 2 {
		t.Fatalf("cpu_core_usage: got %d points, want 2", len(cores))
	}
	for _, p := range cores {
		if p.Value != 40 {
			t.Errorf("cpu_core_usage = %v, want 40", p.Value)
		}
	}
	assertRate(t, pts, "cpu_interrupts", 10)
	assertRate(t, pts, "cpu_context_switches", 20)
}

func TestWarmDifferentBootEmitsNothing(t *testing.T) {
	path := warmPath(t)
	t0 := time.Unix(1000, 0)
	withBoot(t, "boot-1")
	saveState(t, path, t0)

	withBoot(t, "boot-2")
	b := newCollector(t)
	b.SetState(path, time.Minute)
	fakeReaders(b, aggT1, coresT1, kernelT1)
	pts := collectAll(b, t0.Add(30*time.Second))
	assertNoRates(t, pts, "cpu_usage_total", "cpu_core_usage", "cpu_interrupts", "cpu_context_switches")

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file should be removed after SetState, stat err = %v", err)
	}
}

func TestWarmTooOldEmitsNothing(t *testing.T) {
	withBoot(t, "boot-1")
	path := warmPath(t)
	t0 := time.Unix(1000, 0)
	saveState(t, path, t0)

	b := newCollector(t)
	b.SetState(path, time.Minute)
	fakeReaders(b, aggT1, coresT1, kernelT1)
	pts := collectAll(b, t0.Add(121*time.Second))
	assertNoRates(t, pts, "cpu_usage_total", "cpu_core_usage", "cpu_interrupts")
}

func TestWarmMissingOrCorruptFile(t *testing.T) {
	withBoot(t, "boot-1")
	dir := t.TempDir()
	t0 := time.Unix(1000, 0)

	missing := filepath.Join(dir, "missing.json")
	u := newCollector(t)
	u.SetState(missing, time.Minute)
	fakeReaders(u, aggT1, coresT1, kernelT1)
	pts := collectAll(u, t0)
	assertNoRates(t, pts, "cpu_usage_total")

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	v := newCollector(t)
	v.SetState(corrupt, time.Minute)
	fakeReaders(v, aggT1, coresT1, kernelT1)
	pts = collectAll(v, t0)
	assertNoRates(t, pts, "cpu_usage_total")

	if _, err := os.Stat(corrupt); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("corrupt file should be removed, stat err = %v", err)
	}
}

func TestWarmFileUsedOnce(t *testing.T) {
	withBoot(t, "boot-1")
	path := warmPath(t)
	t0 := time.Unix(1000, 0)
	saveState(t, path, t0)

	b := newCollector(t)
	b.SetState(path, time.Minute)
	fakeReaders(b, aggT1, coresT1, kernelT1)
	collectAll(b, t0.Add(30*time.Second))
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	c := newCollector(t)
	c.SetState(path, time.Minute)
	fakeReaders(c, aggT0, coresT0, kernelT0)
	pts := collectAll(c, t0.Add(60*time.Second))
	assertNoRates(t, pts, "cpu_usage_total")
}

func TestWarmCloseWithoutReadingWritesNothing(t *testing.T) {
	withBoot(t, "boot-1")
	path := warmPath(t)

	u := newCollector(t)
	u.SetState(path, time.Minute)
	if err := u.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no file should be written, stat err = %v", err)
	}
}

func TestWarmKernelCounterResetEmitsNoRate(t *testing.T) {
	withBoot(t, "boot-1")
	path := warmPath(t)
	t0 := time.Unix(1000, 0)
	saveState(t, path, t0)

	b := newCollector(t)
	b.SetState(path, time.Minute)
	fakeReaders(b, aggT1, coresT1, kernelCounters{Interrupts: 500, ContextSwitches: 2600, ProcsRunning: 4})
	pts := collectAll(b, t0.Add(30*time.Second))

	assertNoRates(t, pts, "cpu_interrupts")
	assertRate(t, pts, "cpu_context_switches", 20)
}

//go:build !windows

package network

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func withBoot(t *testing.T, id string) {
	t.Helper()
	prev := hostpoll.BootIdentity
	hostpoll.BootIdentity = func() (string, error) { return id, nil }
	t.Cleanup(func() { hostpoll.BootIdentity = prev })
}

func eth0Counters(bytesSent, bytesRecv uint64, packetsSent, packetsRecv uint64) []psnet.IOCountersStat {
	return []psnet.IOCountersStat{{
		Name:        "eth0",
		BytesSent:   bytesSent,
		BytesRecv:   bytesRecv,
		PacketsSent: packetsSent,
		PacketsRecv: packetsRecv,
	}}
}

func fixedCounters(stats []psnet.IOCountersStat) func(bool) ([]psnet.IOCountersStat, error) {
	return func(bool) ([]psnet.IOCountersStat, error) { return stats, nil }
}

func newWarmCollector(t *testing.T) *unixNetworkCollector {
	t.Helper()
	return &unixNetworkCollector{
		logger:     logger.NewLogger(&cliArgs.ParsedArgs{}),
		ioCounters: fixedCounters(eth0Counters(1000, 2000, 10, 20)),
		monitored: func(name string) interfaceInfo {
			return interfaceInfo{isMonitored: true, addresses: []string{"10.0.0.1"}}
		},
		tcpStats:     false,
		lastCounters: map[string]counterWithTime{},
	}
}

func ratePoints(points []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, p := range points {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

func firstRate(t *testing.T, points []data_store.DataPoint, name string) (float64, bool) {
	t.Helper()
	ps := ratePoints(points, name)
	if len(ps) == 0 {
		return 0, false
	}
	return ps[0].Value, true
}

func TestWarmRestartEmitsRatesOnFirstCollection(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")
	t0 := time.Now()

	a := newWarmCollector(t)
	a.ioCounters = fixedCounters(eth0Counters(1000, 2000, 10, 20))
	a.SetState(path, 60*time.Second)
	if _, err := a.Collect(t0); err != nil {
		t.Fatalf("collect A: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}

	b := newWarmCollector(t)
	b.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
	b.SetState(path, 60*time.Second)
	points, err := b.Collect(t0.Add(30 * time.Second))
	if err != nil {
		t.Fatalf("collect B: %v", err)
	}

	for name, want := range map[string]float64{
		"bytes_sent":       100,
		"bytes_received":   100,
		"packets_sent":     1,
		"packets_received": 2,
	} {
		got, ok := firstRate(t, points, name)
		if !ok {
			t.Errorf("missing %s point", name)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestWarmDifferentBootEmitsNothing(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")
	t0 := time.Now()

	a := newWarmCollector(t)
	a.SetState(path, 60*time.Second)
	if _, err := a.Collect(t0); err != nil {
		t.Fatalf("collect A: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}

	hostpoll.BootIdentity = func() (string, error) { return "boot-2", nil }

	b := newWarmCollector(t)
	b.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
	b.SetState(path, 60*time.Second)
	points, err := b.Collect(t0.Add(30 * time.Second))
	if err != nil {
		t.Fatalf("collect B: %v", err)
	}

	for _, name := range []string{"bytes_sent", "bytes_received", "packets_sent", "packets_received"} {
		if ps := ratePoints(points, name); len(ps) > 0 {
			t.Errorf("unexpected %s points on different boot", name)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file should be removed, stat err = %v", err)
	}
}

func TestWarmTooOldEmitsNothing(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")
	t0 := time.Now()

	a := newWarmCollector(t)
	a.SetState(path, 60*time.Second)
	if _, err := a.Collect(t0); err != nil {
		t.Fatalf("collect A: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}

	b := newWarmCollector(t)
	b.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
	b.SetState(path, 60*time.Second)
	points, err := b.Collect(t0.Add(121 * time.Second))
	if err != nil {
		t.Fatalf("collect B: %v", err)
	}

	for _, name := range []string{"bytes_sent", "bytes_received", "packets_sent", "packets_received"} {
		if ps := ratePoints(points, name); len(ps) > 0 {
			t.Errorf("unexpected %s points for stale state", name)
		}
	}
}

func TestWarmMissingOrCorruptFile(t *testing.T) {
	withBoot(t, "boot-1")

	t.Run("missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "network_warm.json")
		u := newWarmCollector(t)
		u.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
		u.SetState(path, 60*time.Second)
		points, err := u.Collect(time.Now())
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		for _, name := range []string{"bytes_sent", "bytes_received", "packets_sent", "packets_received"} {
			if ps := ratePoints(points, name); len(ps) > 0 {
				t.Errorf("unexpected %s points for missing state", name)
			}
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "network_warm.json")
		if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
			t.Fatalf("write corrupt file: %v", err)
		}
		u := newWarmCollector(t)
		u.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
		u.SetState(path, 60*time.Second)
		points, err := u.Collect(time.Now())
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		for _, name := range []string{"bytes_sent", "bytes_received", "packets_sent", "packets_received"} {
			if ps := ratePoints(points, name); len(ps) > 0 {
				t.Errorf("unexpected %s points for corrupt state", name)
			}
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("corrupt state file should be removed, stat err = %v", err)
		}
	})
}

func TestWarmCounterResetEmitsNoRate(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")
	t0 := time.Now()

	a := newWarmCollector(t)
	a.SetState(path, 60*time.Second)
	if _, err := a.Collect(t0); err != nil {
		t.Fatalf("collect A: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}

	b := newWarmCollector(t)
	b.ioCounters = func(bool) ([]psnet.IOCountersStat, error) {
		return []psnet.IOCountersStat{{
			Name:        "eth0",
			BytesSent:   500,
			BytesRecv:   5000,
			PacketsSent: 40,
			PacketsRecv: 80,
		}}, nil
	}
	b.SetState(path, 60*time.Second)
	points, err := b.Collect(t0.Add(30 * time.Second))
	if err != nil {
		t.Fatalf("collect B: %v", err)
	}

	if ps := ratePoints(points, "bytes_sent"); len(ps) > 0 {
		t.Errorf("unexpected bytes_sent point after counter reset")
	}
	if got, ok := firstRate(t, points, "bytes_received"); !ok {
		t.Errorf("missing bytes_received point")
	} else if got != 100 {
		t.Errorf("bytes_received = %v, want 100", got)
	}
}

func TestWarmFileUsedOnce(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")
	t0 := time.Now()

	a := newWarmCollector(t)
	a.SetState(path, 60*time.Second)
	if _, err := a.Collect(t0); err != nil {
		t.Fatalf("collect A: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}

	b := newWarmCollector(t)
	b.ioCounters = fixedCounters(eth0Counters(4000, 5000, 40, 80))
	b.SetState(path, 60*time.Second)
	if _, err := b.Collect(t0.Add(30 * time.Second)); err != nil {
		t.Fatalf("collect B: %v", err)
	}

	c := newWarmCollector(t)
	c.ioCounters = fixedCounters(eth0Counters(7000, 8000, 70, 140))
	c.SetState(path, 60*time.Second)
	points, err := c.Collect(t0.Add(60 * time.Second))
	if err != nil {
		t.Fatalf("collect C: %v", err)
	}
	for _, name := range []string{"bytes_sent", "bytes_received", "packets_sent", "packets_received"} {
		if ps := ratePoints(points, name); len(ps) > 0 {
			t.Errorf("unexpected %s points on third collector", name)
		}
	}
}

func TestWarmCloseWithoutReadingWritesNothing(t *testing.T) {
	withBoot(t, "boot-1")
	path := filepath.Join(t.TempDir(), "network_warm.json")

	u := newWarmCollector(t)
	u.SetState(path, 60*time.Second)
	if err := u.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no state file, stat err = %v", err)
	}
}

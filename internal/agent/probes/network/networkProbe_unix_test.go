//go:build !windows

package network

import (
	"net"
	"testing"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestUnixNetworkCollector_FirstCollectPrimes(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	collector, err := newNetworkCollector(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("newNetworkCollector() error = %v", err)
	}

	u := collector.(*unixNetworkCollector)
	u.primeWait = 10 * time.Millisecond
	u.handover = &hostpoll.Handover[map[string]psnet.IOCountersStat]{}

	now := time.Now()
	dataPoints, err := u.Collect(now)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	monitoredFound := false
	for _, dp := range dataPoints {
		if dp.Name != "bytes_received" {
			continue
		}
		monitoredFound = true
		if dp.Value < 0 {
			t.Errorf("bytes_received = %v, want >= 0", dp.Value)
		}
	}

	for name, last := range u.lastCounters {
		if !last.Timestamp.After(now) {
			t.Errorf("%s: the primed reading must be dated after the collection time, got %v for %v", name, last.Timestamp, now)
		}
	}

	if !monitoredFound {
		t.Skip("no monitored interface emitted bytes_received; loopback is skipped and no other interface is available")
	}
}

// A network probe restarted on a configuration change diffs its first
// rates against the last reading of the instance it replaces, without
// waiting and over the real span since that reading.
func TestARestartedNetworkProbeTakesOverTheLastReading(t *testing.T) {
	name := monitoredInterface(t)
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	elapsed := time.Hour
	const bytesPerSecond = 1000.0
	read := func() ([]psnet.IOCountersStat, error) {
		return []psnet.IOCountersStat{{Name: name, BytesRecv: uint64(elapsed.Seconds() * bytesPerSecond)}}, nil
	}
	handover := &hostpoll.Handover[map[string]psnet.IOCountersStat]{}
	newCollector := func() *unixNetworkCollector {
		c, err := newNetworkCollector(map[string]interface{}{}, baseLogger)
		if err != nil {
			t.Fatal(err)
		}
		u := c.(*unixNetworkCollector)
		u.readCounters = read
		u.sleep = func(d time.Duration) { elapsed += d }
		u.handover = handover
		return u
	}
	boot := time.Unix(1780272000, 0)

	old := newCollector()
	if _, err := old.Collect(boot.Add(elapsed)); err != nil {
		t.Fatal(err)
	}
	elapsed += 30 * time.Second
	if _, err := old.Collect(boot.Add(elapsed)); err != nil {
		t.Fatal(err)
	}

	elapsed += 2 * time.Second
	before := elapsed
	points, err := newCollector().Collect(boot.Add(elapsed))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed != before {
		t.Errorf("the new instance waited %s although an earlier reading was there", elapsed-before)
	}
	for _, p := range points {
		if p.Name == "bytes_received" {
			if p.Value < bytesPerSecond*0.95 || p.Value > bytesPerSecond*1.05 {
				t.Errorf("first bytes_received after the restart = %.1f/s, want about %.0f/s", p.Value, bytesPerSecond)
			}
			return
		}
	}
	t.Fatalf("no bytes_received after the restart: %v", points)
}

// monitoredInterface names an interface of this host that the probe
// monitors by default, or skips the test.
func monitoredInterface(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skip(err)
	}
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	c, err := newNetworkCollector(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatal(err)
	}
	u := c.(*unixNetworkCollector)
	for _, i := range ifaces {
		if info := u.isInterfaceMonitored(i.Name); info.err == nil && info.isMonitored {
			return i.Name
		}
	}
	t.Skip("no interface on this host is monitored by default")
	return ""
}

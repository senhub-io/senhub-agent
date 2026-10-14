package snmppoll

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func mustCIDRs(t *testing.T, ss ...string) []*net.IPNet {
	t.Helper()
	out := make([]*net.IPNet, 0, len(ss))
	for _, s := range ss {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", s, err)
		}
		out = append(out, n)
	}
	return out
}

func hostSeedsRaw(seeds []string, allowed []string) map[string]interface{} {
	seedList := make([]interface{}, 0, len(seeds))
	for _, s := range seeds {
		seedList = append(seedList, s)
	}
	allowedList := make([]interface{}, 0, len(allowed))
	for _, s := range allowed {
		allowedList = append(allowedList, s)
	}

	disc := map[string]interface{}{
		"profile":     map[string]interface{}{"version": "v2c", "community": "public"},
		"max_devices": 200,
		"max_hops":    4,
		"interval":    "1s",
	}
	if seeds != nil {
		disc["seeds"] = seedList
	}
	if allowed != nil {
		disc["allowed_cidrs"] = allowedList
	}
	return map[string]interface{}{
		"target":    "192.0.2.250",
		"mibs":      []interface{}{"mib-2"},
		"discovery": disc,
	}
}

// hostChainFixtures serves gateway A (10.0.0.1) with neighbour B; edits,
// when given, adjust A before it is served.
func hostChainFixtures(f *discoveryFakeFactory, editA ...func(*deviceFixture)) {
	a := deviceFixture{
		serial:           "A-serial",
		vendorPEN:        "9",
		sysName:          "A",
		descr:            "Device A",
		chassisMAC:       []byte{0xAA, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"5": "Gi0/1"},
		neighbors: []fixtureNeighbor{
			{
				mgmtIP:         "10.0.0.2",
				localPort:      "5",
				remotePortName: "Gi0/2",
				remoteSysName:  "B",
				remoteMAC:      []byte{0xBB, 0x01},
			},
		},
	}
	for _, edit := range editA {
		edit(&a)
	}
	f.setDevice("10.0.0.1", a)
	f.setDevice("10.0.0.2", deviceFixture{
		serial:           "B-serial",
		vendorPEN:        "9",
		sysName:          "B",
		descr:            "Device B",
		chassisMAC:       []byte{0xBB, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
	})
}

func assertSeedsMetric(t *testing.T, p *snmppollProbe, source string, want float64) {
	t.Helper()
	points := p.discoveryDataPoints(time.Now())
	for _, dp := range points {
		if dp.Name != metricDiscoverySeeds {
			continue
		}
		for _, tag := range dp.Tags {
			if tag.Key == "source" && tag.Value == source {
				if dp.Value != want {
					t.Fatalf("metric %s source %q = %v, want %v", metricDiscoverySeeds, source, dp.Value, want)
				}
				return
			}
		}
	}
	t.Fatalf("metric %s source %q not found", metricDiscoverySeeds, source)
}

func TestRunDiscovery_HostSeeds(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw(nil, nil)
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return []string{"10.0.0.1"}, mustCIDRs(t, "10.0.0.0/24"), nil
	}

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial"})
}

func TestRunDiscovery_HostGatewayOutsideAllowedIsNotCrawled(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw(nil, []string{"192.168.0.0/24"})
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return []string{"10.0.0.1"}, mustCIDRs(t, "10.0.0.0/24"), nil
	}

	var polls int32
	base := probe.newClient
	probe.newClient = func(cfg *config) snmpClient {
		atomic.AddInt32(&polls, 1)
		return base(cfg)
	}

	probe.runDiscovery(context.Background())

	if got := atomic.LoadInt32(&polls); got != 0 {
		t.Fatalf("expected no polls, got %d", got)
	}
	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{})
}

func TestRunDiscovery_HostScopeErrorFallsBackToConfiguredSeeds(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw([]string{"10.0.0.1"}, []string{"10.0.0.0/24"})
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return nil, nil, errors.New("host scope boom")
	}

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial"})
}

func TestRunDiscovery_NoSeedsNoPoll(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw(nil, nil)
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return nil, nil, nil
	}

	var polls int32
	base := probe.newClient
	probe.newClient = func(cfg *config) snmpClient {
		atomic.AddInt32(&polls, 1)
		return base(cfg)
	}

	probe.runDiscovery(context.Background())

	if got := atomic.LoadInt32(&polls); got != 0 {
		t.Fatalf("expected no polls, got %d", got)
	}
	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{})
}

func TestRunDiscovery_SeedsMetric(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw([]string{"10.0.0.1"}, []string{"10.0.0.0/24"})
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return []string{"10.0.0.2"}, mustCIDRs(t, "10.0.0.0/24"), nil
	}

	probe.runDiscovery(context.Background())

	assertSeedsMetric(t, probe, "configured", 1)
	assertSeedsMetric(t, probe, "host", 1)
}

// TestRunDiscovery_PartialHostScopeStillBounds covers a platform without
// a route reader: no gateway, but the host networks still bound a crawl
// started from a configured seed.
func TestRunDiscovery_PartialHostScopeStillBounds(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f)

	raw := hostSeedsRaw([]string{"10.0.0.1"}, nil)
	probe := newDiscoveryTestProbe(t, raw, f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return nil, mustCIDRs(t, "10.0.0.0/24"), errors.New("no routing table reader on this platform")
	}

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial"})
}

// TestRunDiscovery_GatewayAddressJoinsTheHostRoute checks the crawl started
// from the host's gateway reports that gateway's address as a
// network.address {ip} bound to the device interface. hostnet emits the
// same network.address {ip} node as the next_hop_via of the host's default
// route, so the host and the device meet on it without a new relation.
func TestRunDiscovery_GatewayAddressJoinsTheHostRoute(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	hostChainFixtures(f, func(a *deviceFixture) {
		a.ifNames = map[string]string{"1": "Gi0/0"}
		a.ipAddrs = map[string]string{"10.0.0.1": "1"}
	})

	probe := newDiscoveryTestProbe(t, hostSeedsRaw(nil, nil), f)
	probe.hostScope = func() ([]string, []*net.IPNet, error) {
		return []string{"10.0.0.1"}, mustCIDRs(t, "10.0.0.0/24"), nil
	}
	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	found := false
	for _, r := range obs.Relations {
		if r.Type != relBoundTo || r.FromType != entityTypeNetworkAddress {
			continue
		}
		if r.FromID[idKeyNetworkAddress] == "10.0.0.1" &&
			r.ToID[idKeyNetworkDevice] == "serial:9:A-serial" && r.ToID[idKeyInterfaceName] == "Gi0/0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no network.address 10.0.0.1 bound_to A Gi0/0; relations=%v", obs.Relations)
	}
}

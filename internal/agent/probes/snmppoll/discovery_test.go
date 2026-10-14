package snmppoll

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// discoveryFakeClient is a per-target fake snmpClient that serves raw walks
// from a precomputed map. It is intentionally minimal: the discovery path
// only uses Connect, Close and WalkRaw.
type discoveryFakeClient struct {
	target      string
	connectErr  error
	walkRawData map[string][]snmpRawBind
}

func (c *discoveryFakeClient) Connect() error { return c.connectErr }
func (c *discoveryFakeClient) Get([]string) ([]snmpVarBind, error) {
	return nil, nil
}
func (c *discoveryFakeClient) BulkWalk(string) ([]snmpVarBind, error) {
	return nil, nil
}
func (c *discoveryFakeClient) WalkRaw(base string) ([]snmpRawBind, error) {
	return c.walkRawData[base], nil
}
func (c *discoveryFakeClient) Close() error { return nil }

// discoveryFakeFactory constructs a discoveryFakeClient for each target IP.
type discoveryFakeFactory struct {
	t *testing.T

	mu          sync.Mutex
	data        map[string]map[string][]snmpRawBind // target IP -> walkRawData
	connectErrs map[string]error                    // target IP -> connect error
}

func newDiscoveryFakeFactory(t *testing.T) *discoveryFakeFactory {
	t.Helper()
	return &discoveryFakeFactory{
		t:           t,
		data:        map[string]map[string][]snmpRawBind{},
		connectErrs: map[string]error{},
	}
}

func (f *discoveryFakeFactory) setDevice(ip string, d deviceFixture) {
	f.t.Helper()
	f.data[ip] = d.walkRawData()
}

func (f *discoveryFakeFactory) setConnectError(ip string, err error) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectErrs[ip] = err
}

func (f *discoveryFakeFactory) clientFor(cfg *config) snmpClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.connectErrs[cfg.Target]
	data, ok := f.data[cfg.Target]
	if !ok {
		return &discoveryFakeClient{target: cfg.Target, connectErr: errors.New("no such host")}
	}
	return &discoveryFakeClient{target: cfg.Target, connectErr: err, walkRawData: data}
}

// ---------------------------------------------------------------------------
// fixture construction helpers
// ---------------------------------------------------------------------------

type fixtureNeighbor struct {
	mgmtIP         string
	localPort      string
	remotePortName string
	remoteSysName  string
	remoteMAC      []byte
}

type deviceFixture struct {
	serial           string
	vendorPEN        string
	sysName          string
	descr            string
	engineID         []byte
	chassisMAC       []byte
	services         int
	entPhysicalIndex string
	localPorts       map[string]string // lldpLocPortNum -> port name
	neighbors        []fixtureNeighbor
	ifNames          map[string]string // ifIndex -> ifName
	ipAddrs          map[string]string // IPv4 address -> ifIndex
}

// ipToOctets converts an IPv4 address to its four decimal octets (for the
// lldpRemManAddrTable row index). Returns "" for non-IPv4.
func ipToOctets(ip string) string {
	if p := net.ParseIP(ip); p != nil {
		if p4 := p.To4(); p4 != nil {
			return fmt.Sprintf("%d.%d.%d.%d", p4[0], p4[1], p4[2], p4[3])
		}
	}
	return ""
}

func rawBind(oid string, typ gosnmp.Asn1BER, val any) snmpRawBind {
	return snmpRawBind{OID: oid, Type: typ, Value: val}
}

func (d deviceFixture) walkRawData() map[string][]snmpRawBind {
	data := map[string][]snmpRawBind{}

	// sysObjectID (mandatory reachability + vendor PEN)
	data[oidSysObjectIDBase] = []snmpRawBind{
		rawBind(oidSysObjectID, gosnmp.ObjectIdentifier, "1.3.6.1.4.1."+d.vendorPEN+".1.0"),
	}

	// ENTITY-MIB chassis (serial, class, nameplate)
	if d.entPhysicalIndex != "" {
		idx := d.entPhysicalIndex
		data[oidEntPhysicalClass] = []snmpRawBind{
			rawBind(oidEntPhysicalClass+"."+idx, gosnmp.Integer, entPhysicalClassChassis),
		}
		data[oidEntPhysicalSerialNum] = []snmpRawBind{
			rawBind(oidEntPhysicalSerialNum+"."+idx, gosnmp.OctetString, []byte(d.serial)),
		}
		data[oidEntPhysicalMfgName] = []snmpRawBind{
			rawBind(oidEntPhysicalMfgName+"."+idx, gosnmp.OctetString, []byte("Vendor")),
		}
		data[oidEntPhysicalModelName] = []snmpRawBind{
			rawBind(oidEntPhysicalModelName+"."+idx, gosnmp.OctetString, []byte("Model")),
		}
		data[oidEntPhysicalFirmwareRev] = []snmpRawBind{
			rawBind(oidEntPhysicalFirmwareRev+"."+idx, gosnmp.OctetString, []byte("1.0")),
		}
	}

	// SNMP engine ID (optional fallback)
	if len(d.engineID) > 0 {
		data[oidSnmpEngineIDBase] = []snmpRawBind{
			rawBind(oidSnmpEngineID, gosnmp.OctetString, d.engineID),
		}
	}

	// sysName / sysDescr / sysServices
	data[oidSysNameBase] = []snmpRawBind{
		rawBind(oidSysName, gosnmp.OctetString, []byte(d.sysName)),
	}
	data[oidSysDescrBase] = []snmpRawBind{
		rawBind(oidSysDescr, gosnmp.OctetString, []byte(d.descr)),
	}
	data[oidSysServicesBase] = []snmpRawBind{
		rawBind(oidSysServices, gosnmp.Integer, d.services),
	}

	// LLDP local system + local port table
	lldpLoc := []snmpRawBind{
		rawBind(lldpLocChassisIdSubtype, gosnmp.Integer, subtypeMacAddress),
		rawBind(lldpLocChassisId, gosnmp.OctetString, d.chassisMAC),
		rawBind(lldpLocSysName, gosnmp.OctetString, []byte(d.sysName)),
	}
	for portNum, portName := range d.localPorts {
		lldpLoc = append(lldpLoc,
			rawBind(lldpLocPortIdSubtype+"."+portNum, gosnmp.Integer, portSubtypeIfName),
			rawBind(lldpLocPortId+"."+portNum, gosnmp.OctetString, []byte(portName)),
		)
	}
	data[lldpLocBase] = lldpLoc

	// LLDP remote neighbours + management addresses
	var lldpRem []snmpRawBind
	var lldpMan []snmpRawBind
	for i, n := range d.neighbors {
		rowKey := fmt.Sprintf("0.%s.%d", n.localPort, i+1)
		lldpRem = append(lldpRem,
			rawBind(lldpRemEntry+"."+colRemChassisIdSubtype+"."+rowKey, gosnmp.Integer, subtypeMacAddress),
			rawBind(lldpRemEntry+"."+colRemChassisId+"."+rowKey, gosnmp.OctetString, n.remoteMAC),
			rawBind(lldpRemEntry+"."+colRemPortIdSubtype+"."+rowKey, gosnmp.Integer, portSubtypeIfName),
			rawBind(lldpRemEntry+"."+colRemPortId+"."+rowKey, gosnmp.OctetString, []byte(n.remotePortName)),
			rawBind(lldpRemEntry+"."+colRemSysName+"."+rowKey, gosnmp.OctetString, []byte(n.remoteSysName)),
		)
		if ipOct := ipToOctets(n.mgmtIP); ipOct != "" {
			lldpMan = append(lldpMan,
				rawBind(lldpRemManAddrIfId+".0."+n.localPort+"."+fmt.Sprintf("%d", i+1)+".1.4."+ipOct, gosnmp.Integer, 1),
			)
		}
	}
	if len(lldpRem) > 0 {
		data[lldpRemEntry] = lldpRem
	}
	if len(lldpMan) > 0 {
		data[lldpRemManAddrIfId] = lldpMan
	}

	for idx, name := range d.ifNames {
		data[ifName] = append(data[ifName], rawBind(ifName+"."+idx, gosnmp.OctetString, []byte(name)))
	}
	for ip, idx := range d.ipAddrs {
		n, _ := strconv.Atoi(idx)
		data[ipAdEntIfIndex] = append(data[ipAdEntIfIndex], rawBind(ipAdEntIfIndex+"."+ip, gosnmp.Integer, n))
	}

	return data
}

// baseRawConfig returns a minimal valid raw config with a discovery block.
// max_devices and seeds can be overridden by the caller.
func baseRawDiscoveryConfig(seeds []string, maxDevices int) map[string]interface{} {
	seedList := make([]interface{}, 0, len(seeds))
	for _, s := range seeds {
		seedList = append(seedList, s)
	}
	return map[string]interface{}{
		"target": "10.0.0.250", // probe's own target, separate from the chain
		"mibs":   []interface{}{"mib-2"},
		"discovery": map[string]interface{}{
			"seeds":         seedList,
			"profile":       map[string]interface{}{"version": "v2c", "community": "public"},
			"allowed_cidrs": []interface{}{"10.0.0.0/24"},
			"max_devices":   maxDevices,
			"max_hops":      4,
			"interval":      "1s", // irrelevant for direct runDiscovery calls
		},
	}
}

// newDiscoveryTestProbe builds a probe with the fake factory wired as
// newClient.
func newDiscoveryTestProbe(t *testing.T, raw map[string]interface{}, f *discoveryFakeFactory) *snmppollProbe {
	t.Helper()
	p, err := NewSnmpPollProbe(raw, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewSnmpPollProbe: %v", err)
	}
	probe := p.(*snmppollProbe)
	probe.SetName("snmp-discovery-test")
	probe.newClient = f.clientFor
	return probe
}

// assertDeviceIDs returns the set of network.device ids present in the
// observation.
func assertDeviceIDs(t *testing.T, obs entity.Observation, wantIDs []string) {
	t.Helper()
	got := map[string]int{}
	for _, e := range obs.Entities {
		if e.Type == entityTypeNetworkDevice {
			id, _ := e.ID[idKeyNetworkDevice].(string)
			if id != "" {
				got[id]++
			}
		}
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("device count = %d, want %d; got=%v", len(got), len(wantIDs), got)
	}
	for _, id := range wantIDs {
		if got[id] != 1 {
			t.Errorf("device %q count = %d, want 1", id, got[id])
		}
	}
}

// assertConnectedToCount checks that there are exactly n connected_to relations.
func assertConnectedToCount(t *testing.T, obs entity.Observation, want int) {
	t.Helper()
	count := 0
	for _, r := range obs.Relations {
		if r.Type == relConnectedTo {
			count++
		}
	}
	if count != want {
		t.Fatalf("connected_to relations = %d, want %d", count, want)
	}
}

// assertDiscoveryMetric reads the probe's discovery metrics and checks a
// metric by name and value (for counters, value must be exactly).
func assertDiscoveryMetric(t *testing.T, p *snmppollProbe, name string, want float64) {
	t.Helper()
	points := p.discoveryDataPoints(time.Now())
	for _, dp := range points {
		if dp.Name == name {
			if dp.Value != want {
				t.Errorf("metric %s = %v, want %v", name, dp.Value, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found in %v", name, points)
}

// ---------------------------------------------------------------------------
// test fixtures for the chain
// ---------------------------------------------------------------------------

func standardChainFixtures() *discoveryFakeFactory {
	f := newDiscoveryFakeFactory(nil) // will be replaced in test with t
	// A
	f.setDevice("10.0.0.1", deviceFixture{
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
	})

	// B (also answers on 10.0.0.12 with the same identity for completeness)
	f.setDevice("10.0.0.2", deviceFixture{
		serial:           "B-serial",
		vendorPEN:        "9",
		sysName:          "B",
		descr:            "Device B",
		chassisMAC:       []byte{0xBB, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"6": "Gi0/3"},
		neighbors: []fixtureNeighbor{
			{
				mgmtIP:         "10.0.0.3",
				localPort:      "6",
				remotePortName: "Gi0/4",
				remoteSysName:  "C",
				remoteMAC:      []byte{0xCC, 0x01},
			},
		},
	})

	// C has a neighbour D outside the allowed CIDR; D should never be polled.
	f.setDevice("10.0.0.3", deviceFixture{
		serial:           "C-serial",
		vendorPEN:        "9",
		sysName:          "C",
		descr:            "Device C",
		chassisMAC:       []byte{0xCC, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"7": "Gi0/5"},
		neighbors: []fixtureNeighbor{
			{
				mgmtIP:         "192.168.9.4",
				localPort:      "7",
				remotePortName: "Gi0/1",
				remoteSysName:  "D",
				remoteMAC:      []byte{0xDD, 0x01},
			},
		},
	})

	return f
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// Case 1: one run discovers the chain A-B-C, excludes D (outside CIDR) and
// the probe's own target, produces device entities and connected_to relations,
// and records discovery metrics.
func TestRunDiscovery_OneRun(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	standardChainFixturesInto(f) // helper defined below to avoid duplication

	raw := baseRawDiscoveryConfig([]string{"10.0.0.1"}, 200)
	probe := newDiscoveryTestProbe(t, raw, f)

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	wantIDs := []string{"serial:9:A-serial", "serial:9:B-serial", "serial:9:C-serial"}
	assertDeviceIDs(t, obs, wantIDs)
	// A-B, B-C, and C's LLDP link to D: D is outside allowed_cidrs so it
	// is never polled, but the link C sees is real and is reported.
	assertConnectedToCount(t, obs, 3)

	assertDiscoveryMetric(t, probe, metricDiscoveryDevices, 3)
	assertDiscoveryMetric(t, probe, metricDiscoveryRuns, 1)
}

// Case 2: max_devices=2 stops the crawl at two devices.
func TestRunDiscovery_MaxDevices(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	standardChainFixturesInto(f)

	raw := baseRawDiscoveryConfig([]string{"10.0.0.1"}, 2)
	probe := newDiscoveryTestProbe(t, raw, f)

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	wantIDs := []string{"serial:9:A-serial", "serial:9:B-serial"}
	assertDeviceIDs(t, obs, wantIDs)
	assertDiscoveryMetric(t, probe, metricDiscoveryDevices, 2)
	assertDiscoveryMetric(t, probe, metricDiscoveryRuns, 1)
}

// Case 3: a previously discovered device is kept for discoveryMissGrace runs
// after it stops answering, then removed.
func TestRunDiscovery_MissGrace(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	standardChainFixturesInto(f)

	raw := baseRawDiscoveryConfig([]string{"10.0.0.1"}, 200)
	probe := newDiscoveryTestProbe(t, raw, f)

	// First run: all reachable.
	probe.runDiscovery(context.Background())
	obs := probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial", "serial:9:C-serial"})
	assertDiscoveryMetric(t, probe, metricDiscoveryDevices, 3)
	assertDiscoveryMetric(t, probe, metricDiscoveryFailures, 0) // unreachable metric zero

	// Make C unreachable.
	f.setConnectError("10.0.0.3", errors.New("no route to host"))

	// Run 2: C still retained (misses=1).
	probe.runDiscovery(context.Background())
	obs = probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial", "serial:9:C-serial"})

	// Run 3: still retained (misses=2).
	probe.runDiscovery(context.Background())
	obs = probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial", "serial:9:C-serial"})

	// Run 4: C dropped (misses=3 >= grace).
	probe.runDiscovery(context.Background())
	obs = probe.entitySource.getDiscoveryObservation()
	assertDeviceIDs(t, obs, []string{"serial:9:A-serial", "serial:9:B-serial"})

	// Failures should have been recorded for the unreachable device.
	// Cumulative: runs 2, 3 and 4 each tried C once.
	assertDiscoveryMetric(t, probe, metricDiscoveryFailures, 3)
}

// Case 4: a seed that never answers results in zero devices, an unreachable
// failure, and no panic.
func TestRunDiscovery_UnreachableSeed(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	// no device for 10.0.0.99, so default connect error

	raw := baseRawDiscoveryConfig([]string{"10.0.0.99"}, 200)
	probe := newDiscoveryTestProbe(t, raw, f)

	probe.runDiscovery(context.Background())

	obs := probe.entitySource.getDiscoveryObservation()
	if len(obs.Entities) != 0 {
		t.Fatalf("expected zero entities, got %v", obs.Entities)
	}
	assertDiscoveryMetric(t, probe, metricDiscoveryDevices, 0)
	assertDiscoveryMetric(t, probe, metricDiscoveryFailures, 1) // one unreachable attempt
}

// Case 5: discoveryLoop exits promptly when the context is cancelled.
func TestDiscoveryLoop_Shutdown(t *testing.T) {
	f := newDiscoveryFakeFactory(t)
	f.setDevice("10.0.0.1", deviceFixture{
		serial:           "A-serial",
		vendorPEN:        "9",
		sysName:          "A",
		descr:            "Device A",
		chassisMAC:       []byte{0xAA, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"5": "Gi0/1"},
		neighbors:        nil,
	})

	raw := baseRawDiscoveryConfig([]string{"10.0.0.1"}, 200)
	probe := newDiscoveryTestProbe(t, raw, f)

	ctx, cancel := context.WithCancel(context.Background())
	probe.discoveryWG.Add(1)
	go probe.discoveryLoop(ctx)

	// Give the loop a moment to start and finish its first run.
	time.Sleep(50 * time.Millisecond)

	cancel()
	done := make(chan struct{})
	go func() {
		probe.discoveryWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("discoveryLoop did not return within 1s after cancel")
	}
}

// Case 6: concurrency bound – with 8 seeds the maximum number of in-flight
// Connects never exceeds discoveryMaxConcurrent (4).
func TestRunDiscovery_ConcurrencyBound(t *testing.T) {
	// Build a factory whose clients count in-flight Connect calls.
	inFlight := int32(0)
	maxInFlight := int32(0)

	countingFactory := &discoveryFakeFactory{
		t:           t,
		data:        map[string]map[string][]snmpRawBind{},
		connectErrs: map[string]error{},
	}

	// Create 8 seed devices, each with unique identity and no neighbours.
	seeds := make([]string, 0, 8)
	for i := 1; i <= 8; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		seeds = append(seeds, ip)
		countingFactory.setDevice(ip, deviceFixture{
			serial:           fmt.Sprintf("seed%d-serial", i),
			vendorPEN:        "9",
			sysName:          fmt.Sprintf("seed%d", i),
			descr:            "seed",
			chassisMAC:       []byte{0x10, 0x00, byte(i)},
			services:         0x02,
			entPhysicalIndex: "1",
			localPorts:       nil,
			neighbors:        nil,
		})
	}

	raw := baseRawDiscoveryConfig(seeds, 200)
	probe := newDiscoveryTestProbe(t, raw, countingFactory)
	probe.newClient = func(cfg *config) snmpClient {
		fc := countingFactory.clientFor(cfg).(*discoveryFakeClient)
		return &countingFakeClient{
			discoveryFakeClient: fc,
			inFlight:            &inFlight,
			maxInFlight:         &maxInFlight,
		}
	}

	probe.runDiscovery(context.Background())

	if got := atomic.LoadInt32(&maxInFlight); got > discoveryMaxConcurrent {
		t.Fatalf("max in-flight Connects = %d, want <= %d", got, discoveryMaxConcurrent)
	}
}

// countingFakeClient wraps discoveryFakeClient and instruments Connect.
type countingFakeClient struct {
	*discoveryFakeClient
	inFlight    *int32
	maxInFlight *int32
}

func (c *countingFakeClient) Connect() error {
	cur := atomic.AddInt32(c.inFlight, 1)
	// Update max in-flight (simple CAS loop).
	for {
		max := atomic.LoadInt32(c.maxInFlight)
		if cur <= max || atomic.CompareAndSwapInt32(c.maxInFlight, max, cur) {
			break
		}
	}
	time.Sleep(10 * time.Millisecond)
	atomic.AddInt32(c.inFlight, -1)
	return c.discoveryFakeClient.connectErr
}

// helper to reuse standard chain fixtures in different tests
func standardChainFixturesInto(f *discoveryFakeFactory) {
	f.setDevice("10.0.0.1", deviceFixture{
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
	})

	f.setDevice("10.0.0.2", deviceFixture{
		serial:           "B-serial",
		vendorPEN:        "9",
		sysName:          "B",
		descr:            "Device B",
		chassisMAC:       []byte{0xBB, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"6": "Gi0/3"},
		neighbors: []fixtureNeighbor{
			{
				mgmtIP:         "10.0.0.3",
				localPort:      "6",
				remotePortName: "Gi0/4",
				remoteSysName:  "C",
				remoteMAC:      []byte{0xCC, 0x01},
			},
		},
	})

	f.setDevice("10.0.0.3", deviceFixture{
		serial:           "C-serial",
		vendorPEN:        "9",
		sysName:          "C",
		descr:            "Device C",
		chassisMAC:       []byte{0xCC, 0x01},
		services:         0x02,
		entPhysicalIndex: "1",
		localPorts:       map[string]string{"7": "Gi0/5"},
		neighbors: []fixtureNeighbor{
			{
				mgmtIP:         "192.168.9.4",
				localPort:      "7",
				remotePortName: "Gi0/1",
				remoteSysName:  "D",
				remoteMAC:      []byte{0xDD, 0x01},
			},
		},
	})
}

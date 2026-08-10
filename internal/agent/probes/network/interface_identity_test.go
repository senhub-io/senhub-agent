package network

import (
	"net"
	"sort"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// Regression for #748: a host-interface series must carry the identity key of
// its network.interface entity, so a consumer can pivot from the entity to the
// telemetry that describes it.
//
// Before the fix the only interface-bearing tag was `interface`, which a
// transformer renames to network.interface.name — a label that exists, is
// populated, and joins nothing, because the entity is keyed interface.name.
// The Windows half was worse: `interface` carries the PDH instance name (the
// adapter description) while the entity carries the connection name, so even
// the right key would have joined nothing.
//
// This runs on Unix, where the collector reads the host's real interfaces —
// loopback alone is enough. The Windows path needs a host with PDH counters
// and is covered by recette.
func TestCollectStampsInterfaceNameForTheEntityJoin(t *testing.T) {
	c, err := newNetworkCollector(map[string]interface{}{}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Skipf("no network collector on this host: %v", err)
	}

	// The Unix collector emits rates, so the first cycle only primes the
	// counter cache and returns nothing. Collecting once would make this test
	// silently vacuous — it would skip on every host and never assert.
	if _, err := c.Collect(time.Now()); err != nil {
		t.Skipf("collection unavailable on this host: %v", err)
	}
	points, err := c.Collect(time.Now().Add(time.Second))
	if err != nil {
		t.Skipf("collection unavailable on this host: %v", err)
	}
	if len(points) == 0 {
		t.Skip("no interface datapoints on this host")
	}

	osInterfaceNames := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("cannot enumerate host interfaces: %v", err)
	}
	for _, i := range ifaces {
		osInterfaceNames[i.Name] = true
	}

	checked := 0
	for _, p := range points {
		var iface, identity string
		var hasIface bool
		for _, tag := range p.Tags {
			switch tag.Key {
			case "interface":
				iface, hasIface = tag.Value, true
			case interfaceNameTag:
				identity = tag.Value
			}
		}
		if !hasIface {
			continue // not an interface-scoped series
		}
		checked++
		if identity == "" {
			t.Fatalf("datapoint for interface %q carries no %s; the entity keyed "+
				"on that name has no reachable telemetry (#748)", iface, interfaceNameTag)
		}
		// The assertion is on the identity SOURCE, not on the `interface` tag:
		// on Windows the two legitimately differ — `interface` carries the PDH
		// instance ("Microsoft Hyper-V Network Adapter _3") while the identity
		// is the connection name ("Ethernet 3"), which is the whole point of
		// the fix. What must hold on every platform is that the value equals
		// the name the entity source keys on, i.e. a real net.Interface name.
		if !osInterfaceNames[identity] {
			t.Fatalf("%s = %q is not a net.Interface name on this host (%v); the "+
				"entity is keyed on net.Interface.Name, so this value joins nothing",
				interfaceNameTag, identity, keys(osInterfaceNames))
		}
	}

	if checked == 0 {
		t.Skip("no interface-scoped datapoints to check on this host")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

package network

import (
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

	points, err := c.Collect(time.Now())
	if err != nil {
		t.Skipf("collection unavailable on this host: %v", err)
	}
	if len(points) == 0 {
		t.Skip("no interface datapoints on this host")
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
		// On Unix both derive from the same interface name. The assertion is
		// deliberately on equality rather than presence: a populated tag that
		// names something else is exactly the failure this regression covers.
		if identity != iface {
			t.Fatalf("%s = %q but interface = %q; the identity tag must carry the "+
				"entity's name verbatim", interfaceNameTag, identity, iface)
		}
	}

	if checked == 0 {
		t.Skip("no interface-scoped datapoints to check on this host")
	}
}

package snmppoll

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/entity"
)

const testSelfMgmt = "192.0.2.1"

// testFilterContext publishes 10.1.1.1 on Vlan10 (10.1.1.0/24) and
// 2001:db8::1 is not published (ipAddrTable carries IPv4 only).
func testFilterContext(routes []routeRow) routeFilterContext {
	return newRouteFilterContext(routes,
		[]ipAddr{{IP: "10.1.1.1", IfIndex: "10", Prefix: 24}},
		[]ifaceRow{{Index: "10", Name: "Vlan10"}})
}

func normalized(t *testing.T, r routeRow) routeRow {
	t.Helper()
	nr, ok := eligibleRoute(r, testSelfMgmt)
	if !ok {
		t.Fatalf("route %+v not eligible", r)
	}
	return nr
}

func TestSystemRoute(t *testing.T) {
	learned := "10.0.0.254"
	routes := []routeRow{
		{Destination: "10.1.1.0/24", Type: routeTypeLocal},
		{Destination: "10.2.0.0/30", Type: routeTypeLocal},
	}
	fc := testFilterContext(routes)
	cases := []struct {
		name string
		r    routeRow
		want bool
	}{
		{"reject", routeRow{Destination: "10.9.0.0/16", Type: routeTypeReject}, true},
		{"blackhole", routeRow{Destination: "10.8.0.0/16", Type: routeTypeBlackhole}, true},
		{"multicast v4 block", routeRow{Destination: "224.0.0.0/4", Type: routeTypeLocal}, true},
		{"multicast v4 group", routeRow{Destination: "239.1.2.0/24", Type: routeTypeRemote, NextHop: learned}, true},
		{"multicast v6", routeRow{Destination: "ff02::/16", Type: routeTypeRemote, NextHop: learned}, true},
		{"loopback v4", routeRow{Destination: "127.0.0.0/8", Type: routeTypeLocal}, true},
		{"loopback v6", routeRow{Destination: "::1/128", Type: routeTypeLocal}, true},
		{"link-local v4", routeRow{Destination: "169.254.0.0/16", Type: routeTypeLocal}, true},
		{"link-local v6", routeRow{Destination: "fe80::/64", Type: routeTypeLocal}, true},
		{"unspecified host", routeRow{Destination: "0.0.0.0/32", Type: routeTypeLocal}, true},
		{"limited broadcast", routeRow{Destination: "255.255.255.255/32", Type: routeTypeLocal}, true},
		{"local host, published address", routeRow{Destination: "10.1.1.1/32", Type: routeTypeLocal}, true},
		{"local host, broadcast of connected /24", routeRow{Destination: "10.1.1.255/32", Type: routeTypeLocal}, true},
		{"local host, broadcast of connected /30", routeRow{Destination: "10.2.0.3/32", Type: routeTypeLocal}, true},
		{"local host, address not published", routeRow{Destination: "10.1.1.2/32", Type: routeTypeLocal}, false},
		{"local host v6, not published", routeRow{Destination: "2001:db8::1/128", Type: routeTypeLocal}, false},
		{"connected", routeRow{Destination: "10.1.1.0/24", Type: routeTypeLocal}, false},
		{"ospf", routeRow{Destination: "10.20.0.0/16", Type: routeTypeRemote, NextHop: learned, Protocol: 13}, false},
		{"static", routeRow{Destination: "10.30.0.0/24", Type: routeTypeRemote, NextHop: learned, Protocol: 3}, false},
		{"default v4", routeRow{Destination: "0.0.0.0/0", Type: routeTypeRemote, NextHop: learned}, false},
		{"default v6", routeRow{Destination: "::/0", Type: routeTypeRemote, NextHop: learned}, false},
		{"learned host route", routeRow{Destination: "10.40.0.1/32", Type: routeTypeRemote, NextHop: learned}, false},
		{"learned v6", routeRow{Destination: "2001:db8:5::/48", Type: routeTypeRemote, NextHop: learned}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := systemRoute(normalized(t, c.r), fc); got != c.want {
				t.Errorf("systemRoute(%s) = %v, want %v", c.r.Destination, got, c.want)
			}
		})
	}
}

func filterFixture() []routeRow {
	routes := []routeRow{
		{Destination: "10.50.0.0/24", Type: routeTypeRemote, NextHop: "10.0.0.254", Protocol: 13},
		{Destination: "10.51.0.0/24", Type: routeTypeRemote, NextHop: "10.0.0.254", Protocol: 13},
		{Destination: "10.52.0.0/24", Type: routeTypeRemote, NextHop: "10.0.0.254", Protocol: 13},
		{Destination: "10.1.1.0/24", Type: routeTypeLocal},
		{Destination: "10.1.1.1/32", Type: routeTypeLocal},
		{Destination: "10.1.1.255/32", Type: routeTypeLocal},
		{Destination: "10.9.0.0/16", Type: routeTypeReject},
		{Destination: "224.0.0.0/4", Type: routeTypeLocal},
	}
	for i := 0; i < 8; i++ {
		routes = append(routes, routeRow{Destination: fmt.Sprintf("239.0.%d.0/24", i), Type: routeTypeBlackhole})
	}
	return routes
}

func TestSelectRoutes_FilterBeforeCap(t *testing.T) {
	routes := filterFixture()
	sel := selectRoutes(routes, testSelfMgmt, 4, false, testFilterContext(routes))
	if sel.omitted != 0 {
		t.Errorf("omitted = %d, want 0: filtered routes must not take the bound's slots", sel.omitted)
	}
	if len(sel.filtered) != 12 {
		t.Errorf("filtered = %d, want 12", len(sel.filtered))
	}
	want := map[string]bool{"10.50.0.0/24": true, "10.51.0.0/24": true, "10.52.0.0/24": true, "10.1.1.0/24": true}
	if len(sel.kept) != len(want) {
		t.Fatalf("kept = %+v, want the three learned routes and the connected one", sel.kept)
	}
	for _, r := range sel.kept {
		if !want[r.Destination] {
			t.Errorf("unexpected kept route %s", r.Destination)
		}
	}
}

func TestSelectRoutes_IncludeSystemRoutes(t *testing.T) {
	routes := filterFixture()
	sel := selectRoutes(routes, testSelfMgmt, 100, true, testFilterContext(routes))
	if len(sel.filtered) != 0 || sel.omitted != 0 || len(sel.kept) != len(routes) {
		t.Errorf("kept=%d omitted=%d filtered=%d, want %d/0/0", len(sel.kept), sel.omitted, len(sel.filtered), len(routes))
	}
}

func TestRouteCountersFiltered(t *testing.T) {
	for _, filtered := range []int{0, 12} {
		obs := entity.Observation{Entities: []entity.Entity{deviceEntity("dev1", nil, entity.ScopeSNMPIFMIB)}}
		applyRouteCounters(&obs, "dev1", 2, filtered, 500)
		a := obs.Entities[0].Attributes
		if a[attrRoutesFiltered] != int64(filtered) || a[attrRoutesOmitted] != int64(2) || a[attrRoutesLimit] != int64(500) {
			t.Errorf("counters = %v, want filtered=%d omitted=2 limit=500", a, filtered)
		}
	}
}

func routeSweepClient() *fakeClient {
	route := func(dest, mask, nextHop string, typ int) []snmpRawBind {
		rk := dest + "." + mask + ".0." + nextHop
		return []snmpRawBind{
			{OID: ipCidrRouteEntry + "." + colRouteNextHop + "." + rk, Value: nextHop},
			{OID: ipCidrRouteEntry + "." + colRouteType + "." + rk, Value: typ},
		}
	}
	var routes []snmpRawBind
	routes = append(routes, route("10.1.1.0", "255.255.255.0", "0.0.0.0", routeTypeLocal)...)
	routes = append(routes, route("10.1.1.1", "255.255.255.255", "0.0.0.0", routeTypeLocal)...)
	routes = append(routes, route("10.1.1.2", "255.255.255.255", "0.0.0.0", routeTypeLocal)...)
	routes = append(routes, route("10.1.1.255", "255.255.255.255", "0.0.0.0", routeTypeLocal)...)
	routes = append(routes, route("10.20.0.0", "255.255.0.0", "10.0.0.254", routeTypeRemote)...)
	return &fakeClient{walkRawResult: map[string][]snmpRawBind{
		oidSysNameBase:   {{OID: oidSysName, Value: []byte("r1")}},
		ipCidrRouteEntry: routes,
		ifName:           {rawBind(ifName+".10", gosnmp.OctetString, []byte("Vlan10"))},
		ipAdEntIfIndex:   {rawBind(ipAdEntIfIndex+".10.1.1.1", gosnmp.Integer, 10)},
	}}
}

func observedRoutesAndDevice(t *testing.T, obs entity.Observation) (map[string]bool, entity.Entity) {
	t.Helper()
	dests := map[string]bool{}
	var dev entity.Entity
	for _, e := range obs.Entities {
		switch e.Type {
		case entityTypeNetworkRoute:
			dests[fmt.Sprint(e.ID[idKeyRouteDestination])] = true
		case entityTypeNetworkDevice:
			if e.ID[idKeyNetworkDevice] == "name:r1" {
				dev = e
			}
		}
	}
	if dev.Type == "" {
		t.Fatalf("polled device missing from %+v", obs.Entities)
	}
	return dests, dev
}

func routeEntityFor(deviceID, dest string) entity.Entity {
	return entity.Entity{Type: entityTypeNetworkRoute, ID: map[string]any{
		idKeyNetworkDevice: deviceID, idKeyRouteDestination: dest, attrNextHopIP: "0.0.0.0",
	}}
}

func TestMaybeSweep_FiltersSystemRoutes(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.10", TopologyInterval: 10 * time.Minute}, testLogger(t))
	s.maybeSweep(routeSweepClient(), time.Now())
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("observation not ok after a successful sweep")
	}
	dests, dev := observedRoutesAndDevice(t, obs)
	for _, d := range []string{"10.1.1.0/24", "10.20.0.0/16", "10.1.1.2/32"} {
		if !dests[d] {
			t.Errorf("route %s missing from the observation: %v", d, dests)
		}
	}
	for _, d := range []string{"10.1.1.1/32", "10.1.1.255/32"} {
		if dests[d] {
			t.Errorf("route %s published, want it filtered", d)
		}
	}
	if got := dev.Attributes[attrRoutesFiltered]; got != int64(2) {
		t.Errorf("senhub.routes.filtered = %v, want 2", got)
	}
}

// A route this process published and the filter then leaves out reads, to
// the tracker, as excluded by the configuration, not as having ended.
func TestDepartureReason_FilteredRouteIsExcludedByConfig(t *testing.T) {
	cfg := &config{Target: "192.0.2.10", TopologyInterval: 10 * time.Minute, IncludeSystemRoutes: true}
	s := newEntitySource(cfg, testLogger(t))
	t0 := time.Now()
	s.maybeSweep(routeSweepClient(), t0)
	host := routeEntityFor("name:r1", "10.1.1.1/32")
	learned := entity.Entity{Type: entityTypeNetworkRoute, ID: map[string]any{
		idKeyNetworkDevice: "name:r1", idKeyRouteDestination: "10.20.0.0/16", attrNextHopIP: "10.0.0.254",
	}}
	if got := s.DepartureReason(host); got != "" {
		t.Fatalf("published route: DepartureReason = %q, want none", got)
	}

	cfg.IncludeSystemRoutes = false
	s.maybeSweep(routeSweepClient(), t0.Add(11*time.Minute))
	if got := s.DepartureReason(host); got != entity.ReasonExcludedByConfig {
		t.Errorf("filtered route: DepartureReason = %q, want %q", got, entity.ReasonExcludedByConfig)
	}
	if got := s.DepartureReason(learned); got != "" {
		t.Errorf("kept route: DepartureReason = %q, want none (terminated if it goes)", got)
	}
	if got := s.DepartureReason(routeEntityFor("name:other", "10.1.1.1/32")); got != "" {
		t.Errorf("route of another device: DepartureReason = %q, want none", got)
	}
}

func TestMaybeSweep_IncludeSystemRoutes(t *testing.T) {
	s := newEntitySource(&config{Target: "192.0.2.10", TopologyInterval: 10 * time.Minute, IncludeSystemRoutes: true}, testLogger(t))
	s.maybeSweep(routeSweepClient(), time.Now())
	obs, ok := s.Observe()
	if !ok {
		t.Fatal("observation not ok after a successful sweep")
	}
	dests, dev := observedRoutesAndDevice(t, obs)
	for _, d := range []string{"10.1.1.1/32", "10.1.1.255/32", "10.1.1.2/32", "10.20.0.0/16"} {
		if !dests[d] {
			t.Errorf("route %s missing with include_system_routes: %v", d, dests)
		}
	}
	if got := dev.Attributes[attrRoutesFiltered]; got != int64(0) {
		t.Errorf("senhub.routes.filtered = %v, want 0", got)
	}
}

func TestParseConfigIncludeSystemRoutes(t *testing.T) {
	for _, c := range []struct {
		raw     any
		want    bool
		wantErr bool
	}{{nil, false, false}, {true, true, false}, {false, false, false}, {"maybe", false, true}} {
		raw := map[string]any{"target": "192.0.2.10", "mibs": []any{"mib-2"}}
		if c.raw != nil {
			raw["include_system_routes"] = c.raw
		}
		cfg, err := parseConfig(raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("include_system_routes=%v: want an error", c.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("include_system_routes=%v: %v", c.raw, err)
		}
		if cfg.IncludeSystemRoutes != c.want {
			t.Errorf("include_system_routes=%v: got %v", c.raw, cfg.IncludeSystemRoutes)
		}
	}
}

func newRouteRetireSource(t *testing.T) (*snmpEntitySource, *recordedDeletes) {
	t.Helper()
	rec := &recordedDeletes{}
	s := newEntitySource(&config{Target: "192.0.2.10", TopologyInterval: 10 * time.Minute}, testLogger(t))
	s.registry = newPolledRegistry()
	s.registry.warmupOverride = time.Nanosecond
	s.publish = rec.publish
	return s, rec
}

func retiredRouteDests(rec *recordedDeletes) map[string]int {
	got := map[string]int{}
	for _, ev := range rec.events {
		if ev.Kind == entity.EntityDelete && ev.Entity != nil && ev.Entity.Type == entityTypeNetworkRoute {
			got[fmt.Sprint(ev.Entity.ID[idKeyRouteDestination])+" "+ev.DeleteReason]++
		}
	}
	return got
}

// The first complete read retires, as excluded_by_config and for three
// cycles, the routes an agent without the filter would have published; a
// route still published is never retired.
func TestRetirePreviousVersionRoutes_FirstCompleteSweep(t *testing.T) {
	s, rec := newRouteRetireSource(t)
	t0 := time.Now()
	s.maybeSweep(routeSweepClient(), t0)
	for i := 0; i < entity.RekeyAnnounceCycles+1; i++ {
		s.Observe()
	}
	got := retiredRouteDests(rec)
	want := map[string]int{
		"10.1.1.1/32 " + entity.ReasonExcludedByConfig:   entity.RekeyAnnounceCycles,
		"10.1.1.255/32 " + entity.ReasonExcludedByConfig: entity.RekeyAnnounceCycles,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("retired routes = %v, want %v", got, want)
	}

	rec.events = nil
	s.maybeSweep(routeSweepClient(), t0.Add(11*time.Minute))
	for i := 0; i < entity.RekeyAnnounceCycles+1; i++ {
		s.Observe()
	}
	if got := retiredRouteDests(rec); len(got) != 0 {
		t.Errorf("a later sweep retired again: %v", got)
	}
}

// A route the filter keeps is never retired, even though an agent without
// the filter would have published it too.
func TestRetirePreviousVersionRoutes_KeptRouteNeverRetired(t *testing.T) {
	s, rec := newRouteRetireSource(t)
	s.maybeSweep(routeSweepClient(), time.Now())
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	for k := range retiredRouteDests(rec) {
		for _, kept := range []string{"10.1.1.0/24 ", "10.20.0.0/16 ", "10.1.1.2/32 "} {
			if strings.HasPrefix(k, kept) {
				t.Errorf("retired a route still published: %s", k)
			}
		}
	}
}

// An incomplete first read retires nothing: the current set would be too
// small and the subtraction would retire routes still published.
func TestRetirePreviousVersionRoutes_IncompleteFirstSweepRetiresNothing(t *testing.T) {
	s, rec := newRouteRetireSource(t)
	t0 := time.Now()
	partial := failingWalks{routeSweepClient(), map[string]bool{ipAdEntIfIndex: true}}
	s.maybeSweep(partial, t0)
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	if got := retiredRouteDests(rec); len(got) != 0 {
		t.Fatalf("an incomplete read retired %v", got)
	}
	s.maybeSweep(routeSweepClient(), t0.Add(11*time.Minute))
	for i := 0; i < entity.RekeyAnnounceCycles; i++ {
		s.Observe()
	}
	if got := retiredRouteDests(rec); len(got) != 2 {
		t.Errorf("the first complete read retired %v, want the two system routes", got)
	}
}

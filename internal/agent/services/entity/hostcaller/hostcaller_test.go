package hostcaller

import (
	"fmt"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity/hostdep"
	"senhub-agent.go/internal/agent/services/exporthook"
)

const testHost = "host-1"

var t0 = time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)

type sink struct{ recs []agentstate.LogRecord }

func (s *sink) publish(r agentstate.LogRecord) { s.recs = append(s.recs, r) }

func (s *sink) byEvent(event string) []agentstate.LogRecord {
	var out []agentstate.LogRecord
	for _, r := range s.recs {
		if r.EventName == event {
			out = append(out, r)
		}
	}
	return out
}

func newTracker(s *sink, threshold int, mode PublicMode, ifaces gnet.InterfaceStatList) *Tracker {
	return New(Config{
		Threshold:  threshold,
		Public:     mode,
		Publish:    s.publish,
		Interfaces: func() (gnet.InterfaceStatList, error) { return ifaces, nil },
	})
}

func row(port uint32, caller string, callerPort uint32) hostdep.InboundRow {
	return hostdep.InboundRow{LocalPort: port, LocalIP: "10.0.0.10", CallerIP: caller, CallerPort: callerPort}
}

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func classes(t *testing.T, tr *Tracker, port int) map[string]int {
	t.Helper()
	out := map[string]int{}
	endpoint := fmt.Sprintf("%s:%d/tcp", testHost, port)
	for _, r := range tr.SelfRecords() {
		if r.Attributes[attrEndpoint] != endpoint {
			continue
		}
		if r.Name != MetricCount || r.Unit != "{address}" || r.Type != "gauge" || r.Attributes[attrTransport] != "tcp" {
			t.Fatalf("unexpected record shape: %+v", r)
		}
		out[r.Attributes[attrClass]] = int(r.Value)
	}
	return out
}

func intAttr(t *testing.T, r agentstate.LogRecord, key string) int64 {
	t.Helper()
	v, ok := r.TypedAttributes[key].(int64)
	if !ok {
		t.Fatalf("%s is not an int64 on %s: %#v", key, r.EventName, r.TypedAttributes[key])
	}
	return v
}

func TestSeenGonePresence(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 3, PublicClear, nil)

	for i := 0; i < 3; i++ {
		tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40000), row(443, "10.1.1.1", 40001)}, at(5*i))
		want := 0
		if i == 2 {
			want = 1
		}
		if got := len(s.byEvent(EventSeen)); got != want {
			t.Fatalf("scrape %d: %d seen records, want %d", i, got, want)
		}
	}
	seen := s.byEvent(EventSeen)[0]
	if !seen.Timestamp.Equal(at(0)) {
		t.Errorf("seen Timestamp = %v, want first observation %v", seen.Timestamp, at(0))
	}
	if !seen.ObservedTimestamp.Equal(at(10)) {
		t.Errorf("seen ObservedTimestamp = %v, want emission %v", seen.ObservedTimestamp, at(10))
	}
	if intAttr(t, seen, attrConnCount) != 2 || intAttr(t, seen, attrServerPort) != 443 {
		t.Errorf("seen attrs: %#v", seen.TypedAttributes)
	}
	if seen.Attributes[attrEndpoint] != "host-1:443/tcp" || seen.Attributes[attrTransport] != "tcp" || seen.Attributes[attrClient] != "10.1.1.1" || seen.Attributes[attrNetworkType] != "ipv4" {
		t.Errorf("seen attrs: %#v", seen.Attributes)
	}

	// The miss tolerance is the one of hostdep: 15 misses are kept, the 16th
	// retires the caller.
	forget := hostdep.ForgetAfter(3)
	for i := 1; i <= forget; i++ {
		tr.Observe(testHost, nil, at(10+5*i))
		if n := len(s.byEvent(EventGone)); n != 0 {
			t.Fatalf("gone after %d misses, tolerance is %d", i, forget)
		}
	}
	tr.Observe(testHost, nil, at(10+5*(forget+1)))
	gone := s.byEvent(EventGone)
	if len(gone) != 1 {
		t.Fatalf("%d gone records after tolerance, want 1", len(gone))
	}
	g := gone[0]
	if !g.Timestamp.Equal(at(10)) {
		t.Errorf("gone Timestamp = %v, want last observation %v", g.Timestamp, at(10))
	}
	if !g.ObservedTimestamp.Equal(at(10 + 5*(forget+1))) {
		t.Errorf("gone ObservedTimestamp = %v", g.ObservedTimestamp)
	}
	if d, _ := g.TypedAttributes[attrPresence].(float64); d != 600 {
		t.Errorf("presence.duration = %v, want 600", g.TypedAttributes[attrPresence])
	}
	if intAttr(t, g, attrConnCount) != 0 {
		t.Errorf("gone connection.count = %v, want 0", g.TypedAttributes[attrConnCount])
	}
}

func TestMissBelowToleranceKeepsCaller(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 3, PublicClear, nil)
	for i := 0; i < 3; i++ {
		tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.1.1.1", 40000)}, at(5*i))
	}
	tr.Observe(testHost, nil, at(15))
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.1.1.1", 40000)}, at(20))
	if len(s.byEvent(EventSeen)) != 1 || len(s.byEvent(EventGone)) != 0 {
		t.Fatalf("seen=%d gone=%d, a tolerated miss must not bounce the caller", len(s.byEvent(EventSeen)), len(s.byEvent(EventGone)))
	}
}

func TestNoRecordBeforeDebounce(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 3, PublicClear, nil)
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.1.1.1", 40000)}, at(0))
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.1.1.1", 40000)}, at(5))
	if len(s.recs) != 0 {
		t.Fatalf("%d records before the debounce is met", len(s.recs))
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 1 {
		t.Errorf("a caller inside the debounce counts as emitted: %v", c)
	}
}

func TestActivityPerWindow(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 3, PublicClear, nil)
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40000), row(443, "10.1.1.1", 40001)}, at(0))
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40000), row(443, "10.1.1.1", 40001), row(443, "10.1.1.1", 40002)}, at(5))
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40002)}, at(10))
	if len(s.byEvent(EventActivity)) != 0 {
		t.Fatal("activity emitted before the window ended")
	}
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40002)}, at(15))
	acts := s.byEvent(EventActivity)
	if len(acts) != 1 {
		t.Fatalf("%d activity records, want one per caller per window", len(acts))
	}
	a := acts[0]
	if !a.Timestamp.Equal(at(15)) {
		t.Errorf("activity Timestamp = %v, want window end %v", a.Timestamp, at(15))
	}
	if intAttr(t, a, attrObserved) != 3 || intAttr(t, a, attrMax) != 3 || intAttr(t, a, attrConnCount) != 1 {
		t.Errorf("activity attrs: %#v", a.TypedAttributes)
	}
	if d, _ := a.TypedAttributes[attrWindow].(float64); d != 900 {
		t.Errorf("window.duration = %v, want 900", a.TypedAttributes[attrWindow])
	}

	// The next window starts clean: one connection, seen at 10:15, 10:20, 10:25.
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40002)}, at(20))
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40002)}, at(25))
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 40002)}, at(30))
	acts = s.byEvent(EventActivity)
	if len(acts) != 2 {
		t.Fatalf("%d activity records after two windows", len(acts))
	}
	if intAttr(t, acts[1], attrObserved) != 1 || intAttr(t, acts[1], attrMax) != 1 {
		t.Errorf("second window attrs: %#v", acts[1].TypedAttributes)
	}
}

func TestCapFrozenAndFreedSlotGoesToFirstAppearance(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicClear, nil)
	var rows []hostdep.InboundRow
	for i := 1; i <= 40; i++ {
		rows = append(rows, row(80, fmt.Sprintf("10.0.5.%d", i), 40000))
	}
	tr.Observe(testHost, rows, at(0))
	if n := len(s.byEvent(EventSeen)); n != MaxCallersPerListener {
		t.Fatalf("%d seen records, want the cap %d", n, MaxCallersPerListener)
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 32 || c[ClassOverCap] != 8 {
		t.Fatalf("classes = %v", c)
	}

	// A later arrival with a lower address does not take a place: full is full.
	rows = append(rows, row(80, "10.0.1.1", 40000))
	tr.Observe(testHost, rows, at(5))
	if n := len(s.byEvent(EventSeen)); n != 32 {
		t.Fatalf("%d seen records after a late arrival on a full listener", n)
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 32 || c[ClassOverCap] != 9 {
		t.Fatalf("classes = %v", c)
	}

	// 10.0.5.1 stays away past the tolerance; its place goes to the candidate
	// that appeared first (10.0.5.33, by address among the first-scrape ones),
	// not to 10.0.1.1 whose address is lower but which came later.
	rest := rows[1:]
	forget := hostdep.ForgetAfter(1)
	for i := 1; i <= forget+1; i++ {
		tr.Observe(testHost, rest, at(5+5*i))
	}
	gone := s.byEvent(EventGone)
	if len(gone) != 1 || gone[0].Attributes[attrClient] != "10.0.5.1" {
		t.Fatalf("gone records: %+v", gone)
	}
	seen := s.byEvent(EventSeen)
	if len(seen) != 33 || seen[32].Attributes[attrClient] != "10.0.5.33" {
		t.Fatalf("freed place went to %v (%d seen)", seen[len(seen)-1].Attributes[attrClient], len(seen))
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 32 || c[ClassOverCap] != 8 {
		t.Fatalf("classes after the hand-over = %v", c)
	}
}

func TestLocalClassIsNeverReported(t *testing.T) {
	bridge := gnet.InterfaceStatList{
		{Name: "br-1a2b3c4d5e6f", Addrs: gnet.InterfaceAddrList{{Addr: "192.168.200.1/24"}}},
		{Name: "veth9f8e7d", Addrs: gnet.InterfaceAddrList{{Addr: "192.168.210.1/30"}}},
		{Name: "eth0", Addrs: gnet.InterfaceAddrList{{Addr: "10.0.0.10/24"}}},
	}
	cases := []struct {
		name    string
		caller  string
		local   string
		isLocal bool
	}{
		{"loopback v4", "127.0.0.1", "10.0.0.10", true},
		{"loopback v6", "::1", "10.0.0.10", true},
		{"link-local v4", "169.254.7.7", "10.0.0.10", true},
		{"link-local v6", "fe80::1", "10.0.0.10", true},
		{"wildcard", "0.0.0.0", "10.0.0.10", true},
		{"docker default bridge", "172.17.0.5", "10.0.0.10", true},
		{"podman default network", "10.88.0.4", "10.0.0.10", true},
		{"inside a user bridge subnet", "192.168.200.7", "10.0.0.10", true},
		{"inside a veth subnet", "192.168.210.2", "10.0.0.10", true},
		{"arrives on a bridge address", "10.9.9.9", "192.168.200.1", true},
		{"private on the real interface", "10.9.9.9", "10.0.0.10", false},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", "10.0.0.10", true},
		{"the host's own address", "10.0.0.10", "10.0.0.10", true},
		{"the host's own address, other interface", "10.0.0.10", "192.168.200.1", true},
		{"a neighbour on the real interface", "10.0.0.11", "10.0.0.10", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &sink{}
			tr := newTracker(s, 1, PublicClear, bridge)
			tr.Observe(testHost, []hostdep.InboundRow{{LocalPort: 80, LocalIP: tc.local, CallerIP: tc.caller, CallerPort: 4000}}, at(0))
			c := classes(t, tr, 80)
			if tc.isLocal {
				if len(s.recs) != 0 || c[ClassLocal] != 1 || c[ClassEmitted] != 0 {
					t.Fatalf("records=%d classes=%v, want none and local=1", len(s.recs), c)
				}
				return
			}
			if len(s.recs) != 1 || c[ClassEmitted] != 1 || c[ClassLocal] != 0 {
				t.Fatalf("records=%d classes=%v, want one and emitted=1", len(s.recs), c)
			}
		})
	}
}

func TestPublicModes(t *testing.T) {
	rows := []hostdep.InboundRow{row(443, "8.8.8.8", 5000), row(443, "2001:4860:4860::8888", 5001), row(443, "10.1.1.1", 5002)}
	cases := []struct {
		mode       PublicMode
		records    int
		client4    string
		prefix4    string
		prefix6    string
		classes    map[string]int
		internalOK bool
	}{
		{PublicClear, 3, "8.8.8.8", "", "", map[string]int{ClassEmitted: 3, ClassPublic: 0}, true},
		{PublicTruncated, 3, "", "8.8.8.0/24", "2001:4860:4860::/48", map[string]int{ClassEmitted: 3, ClassPublic: 0}, true},
		{PublicCount, 1, "", "", "", map[string]int{ClassEmitted: 1, ClassPublic: 2}, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			s := &sink{}
			tr := newTracker(s, 1, tc.mode, nil)
			tr.Observe(testHost, rows, at(0))
			seen := s.byEvent(EventSeen)
			if len(seen) != tc.records {
				t.Fatalf("%d records, want %d", len(seen), tc.records)
			}
			var got4, gotPrefix4, gotPrefix6 string
			var hasInternal bool
			for _, r := range seen {
				switch {
				case r.Attributes[attrClient] == "10.1.1.1":
					hasInternal = true
					if r.Attributes[attrPrefix] != "" {
						t.Errorf("an internal address is never truncated: %v", r.Attributes)
					}
				case r.Attributes[attrClient] == "8.8.8.8":
					got4 = "8.8.8.8"
				case r.Attributes[attrNetworkType] == "ipv4":
					gotPrefix4 = r.Attributes[attrPrefix]
				case r.Attributes[attrNetworkType] == "ipv6" && r.Attributes[attrPrefix] != "":
					gotPrefix6 = r.Attributes[attrPrefix]
				}
				if _, ok := r.Attributes[attrClient]; !ok && r.Attributes[attrPrefix] == "" {
					t.Errorf("record carries neither an address nor a prefix: %v", r.Attributes)
				}
				if r.Attributes[attrClient] == "2001:4860:4860::8888" && tc.mode != PublicClear {
					t.Errorf("public address leaked in %s mode", tc.mode)
				}
			}
			if got4 != tc.client4 || gotPrefix4 != tc.prefix4 || gotPrefix6 != tc.prefix6 || hasInternal != tc.internalOK {
				t.Errorf("client4=%q prefix4=%q prefix6=%q internal=%v", got4, gotPrefix4, gotPrefix6, hasInternal)
			}
			c := classes(t, tr, 443)
			for k, v := range tc.classes {
				if c[k] != v {
					t.Errorf("class %s = %d, want %d (%v)", k, c[k], v, c)
				}
			}
		})
	}
}

func TestParsePublicMode(t *testing.T) {
	for in, want := range map[string]PublicMode{"": PublicClear, "clear": PublicClear, "truncated": PublicTruncated, "count": PublicCount, "trunkated": PublicCount, "CLEAR": PublicCount} {
		if got := ParsePublicMode(in); got != want {
			t.Errorf("ParsePublicMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoClientPortAnywhere(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicTruncated, nil)
	rows := []hostdep.InboundRow{row(443, "10.1.1.1", 51111), row(443, "8.8.8.8", 52222)}
	for i := 0; i < 5; i++ {
		tr.Observe(testHost, rows, at(5*i))
	}
	tr.Observe(testHost, nil, at(200))
	if len(s.recs) == 0 {
		t.Fatal("no records to check")
	}
	for _, r := range s.recs {
		for _, m := range []map[string]string{r.Attributes} {
			for k, v := range m {
				if k == "client.port" || k == "network.peer.address" || k == "network.peer.port" || v == "51111" || v == "52222" {
					t.Errorf("%s carries %s=%s", r.EventName, k, v)
				}
			}
		}
		for k, v := range r.TypedAttributes {
			if k == "client.port" || v == int64(51111) || v == int64(52222) {
				t.Errorf("%s carries typed %s=%v", r.EventName, k, v)
			}
		}
	}
}

func TestNothingToReportMeansNoRecord(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicClear, nil)
	for i := 0; i < 10; i++ {
		tr.Observe(testHost, nil, at(5*i))
	}
	if len(s.recs) != 0 || len(tr.SelfRecords()) != 0 {
		t.Fatalf("records=%d metrics=%d with no inbound row", len(s.recs), len(tr.SelfRecords()))
	}
}

func TestClassesPartitionDistinctAddresses(t *testing.T) {
	bridge := gnet.InterfaceStatList{{Name: "docker0", Addrs: gnet.InterfaceAddrList{{Addr: "172.30.0.1/16"}}}}
	for _, mode := range []PublicMode{PublicClear, PublicTruncated, PublicCount} {
		t.Run(string(mode), func(t *testing.T) {
			s := &sink{}
			tr := newTracker(s, 3, mode, bridge)
			var rows []hostdep.InboundRow
			distinct := map[string]bool{}
			add := func(addr string, n int) {
				distinct[addr] = true
				for i := 0; i < n; i++ {
					rows = append(rows, row(8080, addr, uint32(30000+len(rows))))
				}
			}
			for i := 1; i <= 36; i++ {
				add(fmt.Sprintf("10.2.0.%d", i), 1+i%3)
			}
			add("8.8.4.4", 2)
			add("1.1.1.1", 1)
			add("127.0.0.1", 4)
			add("fe80::9", 1)
			add("172.30.4.4", 1)
			for i := 0; i < 4; i++ {
				tr.Observe(testHost, rows, at(5*i))
				c := classes(t, tr, 8080)
				sum := 0
				for _, v := range c {
					sum += v
				}
				if sum != len(distinct) {
					t.Fatalf("scrape %d: classes %v sum to %d, distinct addresses %d", i, c, sum, len(distinct))
				}
				if c[ClassLocal] != 3 {
					t.Fatalf("local = %d, want 3 (%v)", c[ClassLocal], c)
				}
				if mode != PublicCount && c[ClassPublic] != 0 {
					t.Fatalf("public class used in %s mode: %v", mode, c)
				}
				if mode == PublicCount && c[ClassPublic] != 2 {
					t.Fatalf("public = %d in count mode, want 2", c[ClassPublic])
				}
			}
		})
	}
}

func TestListenersAreKeyedByPort(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicClear, nil)
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.1.1.1", 1), row(443, "10.1.1.1", 2)}, at(0))
	seen := s.byEvent(EventSeen)
	if len(seen) != 2 || seen[0].Attributes[attrEndpoint] == seen[1].Attributes[attrEndpoint] {
		t.Fatalf("one caller on two listeners must give two records with their own endpoint: %+v", seen)
	}
	for _, r := range seen {
		if r.EventName != EventSeen || len(r.TargetStrategies) != 1 || r.TargetStrategies[0] != "otlp" {
			t.Errorf("record routing/event: %+v", r)
		}
	}
}

func TestInstallExportsTheMetricUntilRemoved(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicClear, nil)
	tr.Observe(testHost, []hostdep.InboundRow{row(443, "10.1.1.1", 1)}, at(0))

	count := func() int {
		n := 0
		for _, r := range exporthook.SelfRecords(at(0)) {
			if r.Name == MetricCount {
				n++
			}
		}
		return n
	}
	if count() != 0 {
		t.Fatal("metric exported before a tracker was installed")
	}
	uninstall := Install(tr)
	if got := count(); got != 4 {
		t.Fatalf("%d series, want the four classes of one listener", got)
	}
	uninstall()
	if count() != 0 {
		t.Fatal("metric still exported after uninstall")
	}
}

func newCappedTracker(s *sink, threshold, cap int) *Tracker {
	return New(Config{Threshold: threshold, Public: PublicClear, Publish: s.publish, Cap: cap,
		Interfaces: func() (gnet.InterfaceStatList, error) { return nil, nil }})
}

func TestLateAdmissionKeepsTheFirstSighting(t *testing.T) {
	s := &sink{}
	tr := newCappedTracker(s, 1, 1)
	both := []hostdep.InboundRow{row(80, "10.0.0.1", 1), row(80, "10.0.0.2", 2)}
	tr.Observe(testHost, both, at(0))
	only2 := []hostdep.InboundRow{row(80, "10.0.0.2", 2)}
	for i := 1; i <= hostdep.ForgetAfter(1)+1; i++ {
		tr.Observe(testHost, only2, at(5*i))
	}
	seen := s.byEvent(EventSeen)
	if len(seen) != 2 || seen[1].Attributes[attrClient] != "10.0.0.2" {
		t.Fatalf("seen records: %+v", seen)
	}
	if !seen[1].Timestamp.Equal(at(0)) {
		t.Errorf("late seen Timestamp = %v, want the first sighting %v", seen[1].Timestamp, at(0))
	}
	last := at(5 * (hostdep.ForgetAfter(1) + 1))
	for i := 1; i <= hostdep.ForgetAfter(1)+1; i++ {
		tr.Observe(testHost, nil, last.Add(time.Duration(5*i)*time.Minute))
	}
	gone := s.byEvent(EventGone)
	if len(gone) != 2 {
		t.Fatalf("%d gone records", len(gone))
	}
	if d, _ := gone[1].TypedAttributes[attrPresence].(float64); d != last.Sub(at(0)).Seconds() {
		t.Errorf("presence.duration = %v, want %v", d, last.Sub(at(0)).Seconds())
	}
}

func TestReadGapReportsTheSpanActuallyCovered(t *testing.T) {
	s := &sink{}
	tr := newTracker(s, 1, PublicClear, nil)
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.0.0.1", 1)}, at(0))
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.0.0.1", 1)}, at(5))
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.0.0.1", 1)}, at(180))
	acts := s.byEvent(EventActivity)
	if len(acts) != 1 {
		t.Fatalf("%d activity records", len(acts))
	}
	if !acts[0].Timestamp.Equal(at(5)) {
		t.Errorf("Timestamp = %v, want the end of the covered span %v", acts[0].Timestamp, at(5))
	}
	if d, _ := acts[0].TypedAttributes[attrWindow].(float64); d != 300 {
		t.Errorf("window.duration = %v, want 300", acts[0].TypedAttributes[attrWindow])
	}
}

func TestZeroThresholdFallsBackToHostdepDefault(t *testing.T) {
	tr := New(Config{Threshold: 0})
	if tr.threshold != hostdep.DefaultThreshold || tr.threshold < 1 {
		t.Fatalf("threshold = %d", tr.threshold)
	}
}

func TestUnreadableInterfaceListWarnsOnceAndFallsBackToAddress(t *testing.T) {
	s := &sink{}
	warned := 0
	tr := New(Config{Threshold: 1, Publish: s.publish,
		Interfaces:       func() (gnet.InterfaceStatList, error) { return nil, fmt.Errorf("netlink denied") },
		OnInterfaceError: func(error) { warned++ }})
	rows := []hostdep.InboundRow{row(80, "10.9.9.9", 1), row(80, "127.0.0.1", 2)}
	tr.Observe(testHost, rows, at(0))
	tr.Observe(testHost, rows, at(5))
	if warned != 1 {
		t.Errorf("warned %d times, want once", warned)
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 1 || c[ClassLocal] != 1 {
		t.Errorf("classes = %v: address-only classification must still apply", c)
	}
}

func TestPendingCallersHoldNoPlace(t *testing.T) {
	s := &sink{}
	tr := newCappedTracker(s, 3, 2)
	var burst []hostdep.InboundRow
	for i := 1; i <= 10; i++ {
		burst = append(burst, row(80, fmt.Sprintf("10.0.1.%d", i), 1))
	}
	tr.Observe(testHost, burst, at(0))
	steady := []hostdep.InboundRow{row(80, "10.0.9.1", 1), row(80, "10.0.9.2", 2)}
	tr.Observe(testHost, steady, at(5))
	tr.Observe(testHost, steady, at(10))
	tr.Observe(testHost, steady, at(15))
	seen := s.byEvent(EventSeen)
	if len(seen) != 2 {
		t.Fatalf("%d seen records, want the two steady callers: one-shot callers must not hold the places", len(seen))
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 2 {
		t.Errorf("classes = %v", c)
	}
}

func TestConfirmationBeyondFreePlacesGoesByFirstAppearanceThenAddress(t *testing.T) {
	s := &sink{}
	tr := newCappedTracker(s, 2, 2)
	tr.Observe(testHost, []hostdep.InboundRow{row(80, "10.0.0.9", 1)}, at(0))
	three := []hostdep.InboundRow{row(80, "10.0.0.9", 1), row(80, "10.0.0.3", 2), row(80, "10.0.0.2", 3)}
	tr.Observe(testHost, three, at(5))
	seen := s.byEvent(EventSeen)
	if len(seen) != 1 || seen[0].Attributes[attrClient] != "10.0.0.9" {
		t.Fatalf("seen = %+v, want only the earliest caller", seen)
	}
	tr.Observe(testHost, three, at(10))
	seen = s.byEvent(EventSeen)
	if len(seen) != 2 || seen[1].Attributes[attrClient] != "10.0.0.2" {
		t.Fatalf("seen = %+v, want 10.0.0.2 second (same first appearance as .3, lower address)", seen)
	}
	if c := classes(t, tr, 80); c[ClassEmitted] != 2 || c[ClassOverCap] != 1 {
		t.Errorf("classes = %v", c)
	}
}

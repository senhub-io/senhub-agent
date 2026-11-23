package hostdep

import (
	"net"
	"net/netip"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/netconn"
)

func nc(laddr string, lport uint16, raddr string, rport uint16, inode uint32, rtt time.Duration, retrans uint32, sent, recv uint64) netconn.Conn {
	return netconn.Conn{
		State:      netconn.StateEstablished,
		LocalAddr:  netip.AddrPortFrom(netip.MustParseAddr(laddr), lport),
		RemoteAddr: netip.AddrPortFrom(netip.MustParseAddr(raddr), rport),
		Inode:      inode, HasInfo: true,
		RTT: rtt, Retransmits: retrans, BytesSent: sent, BytesReceived: recv,
	}
}

// edgeSource returns a one-scrape-threshold source with a private registry.
func edgeSource(rows []gnet.ConnectionStat) (*Source, *edgeRegistry) {
	s := newTestSource(rows)
	s.threshold = 1
	s.edges = newEdgeRegistry()
	return s, s.edges
}

func observeOK(t *testing.T, s *Source) {
	t.Helper()
	if _, ok := s.Observe(); !ok {
		t.Fatal("Observe ok=false")
	}
}

func metricFor(ms []EdgeMetric, addr string) *EdgeMetric {
	for i := range ms {
		if ms[i].Attributes[idKeyServerAddress] == addr {
			return &ms[i]
		}
	}
	return nil
}

func TestEdgeMetrics_AttributesAreExactlyTheEdgeKeys(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100),
		conn(statusEstablished, "10.0.0.5", 53001, "127.0.0.1", 6379, 100),
	}
	cases := []struct {
		name     string
		peer     string
		wantKeys []string
	}{
		{"routable target has four keys", "8.8.8.8", []string{idKeyServiceInstanceID, idKeyServerAddress, idKeyServerPort, idKeyNetworkTransport}},
		{"host-local target adds host.id", "127.0.0.1", []string{idKeyServiceInstanceID, idKeyServerAddress, idKeyServerPort, idKeyNetworkTransport, idKeyHost}},
	}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	ms := reg.collect([]netconn.Conn{
		nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, time.Millisecond, 0, 10, 20),
		nc("10.0.0.5", 53001, "127.0.0.1", 6379, 2, time.Millisecond, 0, 10, 20),
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := metricFor(ms, tc.peer)
			if m == nil {
				t.Fatalf("no metric for %s in %+v", tc.peer, ms)
			}
			if len(m.Attributes) != len(tc.wantKeys) {
				t.Errorf("attributes = %v, want keys %v", m.Attributes, tc.wantKeys)
			}
			for _, k := range tc.wantKeys {
				if _, ok := m.Attributes[k]; !ok {
					t.Errorf("missing key %s in %v", k, m.Attributes)
				}
			}
			if m.Attributes[idKeyServiceInstanceID] != "nginx@h-1" {
				t.Errorf("service.instance.id = %q", m.Attributes[idKeyServiceInstanceID])
			}
		})
	}
}

func TestEdgeMetrics_MeanRTTOverSocketsWithInfo(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100),
		conn(statusEstablished, "10.0.0.5", 53001, "8.8.8.8", 443, 100),
		conn(statusEstablished, "10.0.0.5", 53002, "8.8.8.8", 443, 100),
	}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	noInfo := nc("10.0.0.5", 53002, "8.8.8.8", 443, 3, 900*time.Millisecond, 0, 0, 0)
	noInfo.HasInfo = false
	ms := reg.collect([]netconn.Conn{
		nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, 10*time.Millisecond, 0, 0, 0),
		nc("10.0.0.5", 53001, "8.8.8.8", 443, 2, 30*time.Millisecond, 0, 0, 0),
		noInfo,
	})
	m := metricFor(ms, "8.8.8.8")
	if m == nil || !m.HasRTT || m.RTT != 20*time.Millisecond {
		t.Fatalf("mean RTT = %+v, want 20ms", m)
	}
}

func TestEdgeMetrics_CountersMonotonic(t *testing.T) {
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100)}
	steps := []struct {
		name                  string
		conns                 []netconn.Conn
		wantRetrans, wantSent uint64
		wantRecv              uint64
	}{
		{"first sighting contributes its current value",
			[]netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, 0, 2, 1000, 500)}, 2, 1000, 500},
		{"growth adds only the delta",
			[]netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, 0, 3, 1500, 800)}, 3, 1500, 800},
		{"socket closes, no sockets: totals kept",
			nil, 3, 1500, 800},
		{"a new socket opens: its whole value is added",
			[]netconn.Conn{nc("10.0.0.5", 53009, "8.8.8.8", 443, 7, 0, 1, 100, 50)}, 4, 1600, 850},
		{"value goes down (inode reused): nothing added, total not decreasing",
			[]netconn.Conn{nc("10.0.0.5", 53009, "8.8.8.8", 443, 7, 0, 0, 40, 10)}, 4, 1600, 850},
		{"growth resumes from the lower value",
			[]netconn.Conn{nc("10.0.0.5", 53009, "8.8.8.8", 443, 7, 0, 0, 90, 30)}, 4, 1650, 870},
	}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			// hostdep attributes a new socket on its next scrape.
			if len(st.conns) > 0 && st.conns[0].LocalAddr.Port() == 53009 {
				s.connections = fakeConns([]gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53009, "8.8.8.8", 443, 100)})
				observeOK(t, s)
			}
			ms := reg.collect(st.conns)
			m := metricFor(ms, "8.8.8.8")
			if m == nil {
				t.Fatal("edge metric disappeared")
			}
			if m.Retransmits != st.wantRetrans || m.BytesSent != st.wantSent || m.BytesReceived != st.wantRecv {
				t.Errorf("got retrans=%d sent=%d recv=%d, want %d %d %d",
					m.Retransmits, m.BytesSent, m.BytesReceived, st.wantRetrans, st.wantSent, st.wantRecv)
			}
		})
	}
}

func TestEdgeMetrics_ZeroInodeKeysOnFourTuple(t *testing.T) {
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100)}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	reg.collect([]netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 0, 0, 0, 100, 0)})
	ms := reg.collect([]netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 0, 0, 0, 150, 0)})
	if m := metricFor(ms, "8.8.8.8"); m == nil || m.BytesSent != 150 {
		t.Fatalf("sent = %+v, want 150 (not double counted)", m)
	}
}

func TestEdgeMetrics_RetiredEdgeSeriesIsGone(t *testing.T) {
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100)}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	live := []netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, 0, 0, 100, 0)}
	if metricFor(reg.collect(live), "8.8.8.8") == nil {
		t.Fatal("edge must have a metric while emitted")
	}
	// The peer disappears for longer than the miss tolerance: hostdep retires it.
	s.connections = fakeConns(nil)
	for i := 0; i <= earnWindow+1; i++ {
		observeOK(t, s)
	}
	if ms := reg.collect(live); len(ms) != 0 {
		t.Fatalf("retired edge still has metrics: %+v", ms)
	}
	if len(reg.totals) != 0 {
		t.Errorf("totals not dropped: %v", reg.totals)
	}
}

func TestEdgeMetrics_ExcludedPeerHasNoMetric(t *testing.T) {
	rows := []gnet.ConnectionStat{
		conn(statusEstablished, "192.0.2.1", 40000, "10.1.2.3", 443, 5),
		conn(statusEstablished, "192.0.2.1", 40001, "203.0.113.9", 443, 5),
	}
	s, reg := edgeSource(rows)
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	s.exclude = append(s.exclude, ipnet)
	observeOK(t, s)
	ms := reg.collect([]netconn.Conn{
		nc("192.0.2.1", 40000, "10.1.2.3", 443, 1, time.Millisecond, 0, 10, 10),
		nc("192.0.2.1", 40001, "203.0.113.9", 443, 2, time.Millisecond, 0, 10, 10),
	})
	if metricFor(ms, "10.1.2.3") != nil {
		t.Error("excluded peer must have no metric")
	}
	if metricFor(ms, "203.0.113.9") == nil {
		t.Error("kept peer must have a metric")
	}
}

func TestEdgeMetrics_NothingBeforeAnEdgeIsEmitted(t *testing.T) {
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100)}
	s, reg := edgeSource(rows)
	s.threshold = 3
	observeOK(t, s)
	if ms := reg.collect([]netconn.Conn{nc("10.0.0.5", 53000, "8.8.8.8", 443, 1, 0, 0, 1, 1)}); len(ms) != 0 {
		t.Fatalf("pre-debounce edge has metrics: %+v", ms)
	}
}

func TestEdgeMetrics_UnsupportedPlatformYieldsNothing(t *testing.T) {
	// Non-Linux reads return no connections (netconn.ErrUnsupported upstream).
	rows := []gnet.ConnectionStat{conn(statusEstablished, "10.0.0.5", 53000, "8.8.8.8", 443, 100)}
	s, reg := edgeSource(rows)
	observeOK(t, s)
	if ms := reg.collect(nil); len(ms) != 0 {
		t.Fatalf("got %+v, want nothing", ms)
	}
}

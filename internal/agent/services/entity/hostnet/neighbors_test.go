package hostnet

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

func TestParseProcARP(t *testing.T) {
	input := strings.Join([]string{
		"IP address HW type Flags HW address Mask Device",
		"192.168.1.1 0x1 0x2 00:11:22:33:44:55 * eth0",
		"192.168.1.2 0x1 0x4 aa:bb:cc:dd:ee:ff * eth1",
		"192.168.1.3 0x1 0x0 00:00:00:00:00:00 * eth0",
		"192.168.1.4 0x1 0x2 00:00:00:00:00:00 * eth0",
		"bad line",
		"192.168.1.5 0x1 0x2 00:11:22:33:44:55 * eth0",
	}, "\n")

	got := parseProcARP(strings.NewReader(input))
	if len(got) != 3 {
		t.Fatalf("got %d neighbors, want 3: %+v", len(got), got)
	}
	if got[0].IP != "192.168.1.1" || got[0].State != "reachable" || got[0].Iface != "eth0" {
		t.Fatalf("unexpected first neighbor: %+v", got[0])
	}
	if got[1].IP != "192.168.1.2" || got[1].State != "permanent" || got[1].Iface != "eth1" {
		t.Fatalf("unexpected second neighbor: %+v", got[1])
	}
	if got[2].IP != "192.168.1.5" || got[2].State != "reachable" {
		t.Fatalf("unexpected third neighbor: %+v", got[2])
	}

	if got := parseProcARP(strings.NewReader("IP address HW type Flags HW address Mask Device\n")); len(got) != 0 {
		t.Fatalf("header-only parse returned %+v", got)
	}
}

func TestKeepNeighbor(t *testing.T) {
	cases := []struct {
		name string
		n    hostNeighbor
		want bool
	}{
		{"reachable", hostNeighbor{IP: "192.168.1.1", MAC: "00:11:22:33:44:55", Iface: "eth0", State: "reachable"}, true},
		{"incomplete", hostNeighbor{IP: "192.168.1.2", MAC: "00:11:22:33:44:55", State: "incomplete"}, false},
		{"failed", hostNeighbor{IP: "192.168.1.3", MAC: "00:11:22:33:44:55", State: "failed"}, false},
		{"zero mac", hostNeighbor{IP: "192.168.1.4", MAC: "00:00:00:00:00:00", State: "reachable"}, false},
		{"broadcast mac", hostNeighbor{IP: "192.168.1.5", MAC: "ff:ff:ff:ff:ff:ff", State: "reachable"}, false},
		{"multicast mac", hostNeighbor{IP: "192.168.1.6", MAC: "01:00:5e:00:00:01", State: "reachable"}, false},
		{"multicast ip", hostNeighbor{IP: "224.0.0.1", MAC: "00:11:22:33:44:55", State: "reachable"}, false},
		{"unspecified ip", hostNeighbor{IP: "0.0.0.0", MAC: "00:11:22:33:44:55", State: "reachable"}, false},
		{"loopback ip", hostNeighbor{IP: "127.0.0.1", MAC: "00:11:22:33:44:55", State: "reachable"}, false},
		{"ipv6 loopback", hostNeighbor{IP: "::1", MAC: "00:11:22:33:44:55", State: "reachable"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := keepNeighbor(tc.n); got != tc.want {
				t.Fatalf("keepNeighbor(%+v) = %v, want %v", tc.n, got, tc.want)
			}
		})
	}
}

func makeIPNetRow(t *testing.T, family uint16, ipStr string, ifIndex uint32, macStr string, state uint32) []byte {
	t.Helper()
	ip := net.ParseIP(ipStr)
	if ip == nil {
		t.Fatalf("bad test IP %q", ipStr)
	}
	hw, err := net.ParseMAC(macStr)
	if err != nil {
		t.Fatalf("bad test MAC %q: %v", macStr, err)
	}
	row := make([]byte, ipnetRowSize)
	binary.LittleEndian.PutUint16(row[ipnetAddrFamilyOff:], family)
	switch family {
	case afINET:
		copy(row[ipnetV4AddrOff:], ip.To4())
	case afINET6:
		copy(row[ipnetV6AddrOff:], ip.To16())
	default:
		t.Fatalf("bad test family %d", family)
	}
	binary.LittleEndian.PutUint32(row[ipnetInterfaceIndexOff:], ifIndex)
	copy(row[ipnetPhysicalAddressOff:], hw)
	binary.LittleEndian.PutUint32(row[ipnetPhysicalAddressLengthOff:], uint32(len(hw)))
	binary.LittleEndian.PutUint32(row[ipnetStateOff:], state)
	return row
}

func makeIPNetTable(rows ...[]byte) []byte {
	buf := make([]byte, ipnetTableHeaderSize+len(rows)*ipnetRowSize)
	binary.LittleEndian.PutUint32(buf[:4], uint32(len(rows)))
	for i, row := range rows {
		copy(buf[ipnetTableHeaderSize+i*ipnetRowSize:], row)
	}
	return buf
}

func TestParseIpNetTable2(t *testing.T) {
	buf := makeIPNetTable(
		makeIPNetRow(t, afINET, "192.168.1.1", 1, "00:11:22:33:44:55", 5),
		makeIPNetRow(t, afINET6, "2001:db8::2", 2, "aa:bb:cc:dd:ee:ff", 4),
	)
	got := parseIpNetTable2(buf, func(idx int) string {
		if idx == 1 {
			return "eth0"
		}
		return "eth1"
	})
	if len(got) != 2 {
		t.Fatalf("got %d neighbors, want 2: %+v", len(got), got)
	}
	if got[0].IP != "192.168.1.1" || got[0].State != "reachable" || got[0].Iface != "eth0" {
		t.Fatalf("unexpected IPv4 neighbor: %+v", got[0])
	}
	if got[1].IP != "2001:db8::2" || got[1].State != "stale" || got[1].Iface != "eth1" {
		t.Fatalf("unexpected IPv6 neighbor: %+v", got[1])
	}
}

func TestParseIpNetTable2Filters(t *testing.T) {
	buf := makeIPNetTable(
		makeIPNetRow(t, afINET, "192.168.1.3", 1, "00:00:00:00:00:00", 1), // incomplete
		makeIPNetRow(t, afINET, "192.168.1.4", 1, "ff:ff:ff:ff:ff:00", 5), // broadcast-ish bad MAC
		makeIPNetRow(t, afINET, "192.168.1.5", 1, "ff:ff:ff:ff:ff:ff", 5), // broadcast MAC
	)
	got := parseIpNetTable2(buf, nil)
	if len(got) != 0 {
		t.Fatalf("got filtered neighbors, want 0: %+v", got)
	}
}

func TestParseIpNetTable2Truncated(t *testing.T) {
	if got := parseIpNetTable2(make([]byte, ipnetTableHeaderSize-1), nil); len(got) != 0 {
		t.Fatalf("short header returned %+v", got)
	}
	buf := make([]byte, ipnetTableHeaderSize)
	binary.LittleEndian.PutUint32(buf[:4], 1)
	if got := parseIpNetTable2(buf, nil); len(got) != 0 {
		t.Fatalf("missing row returned %+v", got)
	}
}

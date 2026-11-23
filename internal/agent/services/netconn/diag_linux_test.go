//go:build linux

package netconn

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// buildTestNLMsg creates a netlink message with the given type and payload,
// aligned to 4 bytes.
func buildTestNLMsg(t uint16, payload []byte) []byte {
	payload = append([]byte{}, payload...)
	// Pad payload to multiple of 4
	if pad := (len(payload) + 3) &^ 3; pad > len(payload) {
		payload = append(payload, make([]byte, pad-len(payload))...)
	}
	hdr := unix.NlMsghdr{
		Len:   uint32(syscall.NLMSG_HDRLEN + len(payload)),
		Type:  t,
		Flags: 0,
		Seq:   1,
		Pid:   0,
	}
	buf := make([]byte, hdr.Len)
	binary.NativeEndian.PutUint32(buf[0:4], hdr.Len)
	binary.NativeEndian.PutUint16(buf[4:6], hdr.Type)
	binary.NativeEndian.PutUint16(buf[6:8], hdr.Flags)
	binary.NativeEndian.PutUint32(buf[8:12], hdr.Seq)
	binary.NativeEndian.PutUint32(buf[12:16], hdr.Pid)
	copy(buf[16:], payload)
	return buf
}

// buildInetDiagMsg creates an inet_diag_msg byte slice from given parameters.
// addr4/addr6 are expected as netip.Addr; ports as uint16.
// If info is not nil, an INET_DIAG_INFO attribute is appended (full or truncated).
func buildInetDiagMsg(family uint8, state uint8, sport, dport uint16, src, dst netip.Addr, uid, inode uint32, info *unix.TCPInfo, truncInfoLen int) []byte {
	msg := make([]byte, 72)
	msg[0] = family
	msg[1] = state
	binary.BigEndian.PutUint16(msg[4:6], sport)
	binary.BigEndian.PutUint16(msg[6:8], dport)

	if family == unix.AF_INET {
		s := src.As4()
		copy(msg[8:12], s[:])
		d := dst.As4()
		copy(msg[24:28], d[:])
	} else {
		s := src.As16()
		copy(msg[8:24], s[:])
		d := dst.As16()
		copy(msg[24:40], d[:])
	}

	binary.NativeEndian.PutUint32(msg[64:68], uid)
	binary.NativeEndian.PutUint32(msg[68:72], inode)

	if info != nil {
		infoBytes := kernelTCPInfo(info)
		if truncInfoLen > 0 && truncInfoLen < len(infoBytes) {
			infoBytes = infoBytes[:truncInfoLen]
		}
		attrPayload := make([]byte, 0, 4+len(infoBytes))
		rtaLen := uint16(4 + len(infoBytes))
		attr := make([]byte, rtaLen)
		binary.NativeEndian.PutUint16(attr[0:2], rtaLen)
		binary.NativeEndian.PutUint16(attr[2:4], 2) // INET_DIAG_INFO
		copy(attr[4:], infoBytes)
		attrPayload = append(attrPayload, attr...)
		// Pad attribute to 4 bytes
		if pad := (len(attrPayload) + 3) &^ 3; pad < len(attrPayload) {
			attrPayload = append(attrPayload, make([]byte, pad-len(attrPayload))...)
		}
		msg = append(msg, attrPayload...)
	}
	return msg
}

func TestParseDiagMessages(t *testing.T) {
	// IPv4 with tcp_info
	ipv4Src := netip.MustParseAddr("10.0.0.5")
	ipv4Dst := netip.MustParseAddr("10.0.0.9")
	info := unix.TCPInfo{
		Rtt:            1500,
		Total_retrans:  3,
		Bytes_sent:     100,
		Bytes_received: 200,
	}
	msg1 := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, buildInetDiagMsg(unix.AF_INET, 1, 44321, 5432, ipv4Src, ipv4Dst, 1000, 777, &info, 0))

	// IPv6 without attributes
	ipv6Src := netip.MustParseAddr("::1")
	ipv6Dst := netip.MustParseAddr("::1")
	msg2 := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, buildInetDiagMsg(unix.AF_INET6, 10, 8080, 0, ipv6Src, ipv6Dst, 0, 0, nil, 0))

	// NLMSG_DONE
	doneMsg := buildTestNLMsg(unix.NLMSG_DONE, nil)

	buf := append(msg1, append(msg2, doneMsg...)...)

	conns, done, err := parseDiagMessages(buf)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !done {
		t.Fatal("expected done")
	}
	if len(conns) != 2 {
		t.Fatalf("got %d conns, want 2", len(conns))
	}

	c1 := conns[0]
	if c1.Family != unix.AF_INET || c1.State != 1 {
		t.Errorf("bad family/state: %d/%d", c1.Family, c1.State)
	}
	if c1.LocalAddr != netip.AddrPortFrom(ipv4Src, 44321) {
		t.Errorf("bad local addr: %v", c1.LocalAddr)
	}
	if c1.RemoteAddr != netip.AddrPortFrom(ipv4Dst, 5432) {
		t.Errorf("bad remote addr: %v", c1.RemoteAddr)
	}
	if c1.UID != 1000 || c1.Inode != 777 {
		t.Errorf("bad uid/inode: %d/%d", c1.UID, c1.Inode)
	}
	if !c1.HasInfo {
		t.Error("expected HasInfo true")
	}
	if c1.RTT != 1500*time.Microsecond {
		t.Errorf("bad RTT: %v", c1.RTT)
	}
	if c1.Retransmits != 3 || c1.BytesSent != 100 || c1.BytesReceived != 200 {
		t.Errorf("bad metrics: retrans=%d sent=%d recv=%d", c1.Retransmits, c1.BytesSent, c1.BytesReceived)
	}

	c2 := conns[1]
	if c2.Family != unix.AF_INET6 || c2.State != 10 {
		t.Errorf("bad family/state: %d/%d", c2.Family, c2.State)
	}
	if c2.LocalAddr != netip.AddrPortFrom(ipv6Src, 8080) {
		t.Errorf("bad local addr: %v", c2.LocalAddr)
	}
	if c2.HasInfo {
		t.Error("expected HasInfo false")
	}
}

func TestParseTruncatedTCPInfo(t *testing.T) {
	ipv4Src := netip.MustParseAddr("10.0.0.1")
	ipv4Dst := netip.MustParseAddr("10.0.0.2")
	info := unix.TCPInfo{Rtt: 1000, Total_retrans: 5}
	buf := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, buildInetDiagMsg(unix.AF_INET, 1, 12345, 80, ipv4Src, ipv4Dst, 0, 0, &info, 8))
	conns, _, err := parseDiagMessages(buf)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("got %d conns, want 1", len(conns))
	}
	c := conns[0]
	if !c.HasInfo {
		t.Error("expected HasInfo true")
	}
	if c.RTT != 0 {
		t.Errorf("expected RTT 0, got %v", c.RTT)
	}
}

func TestParseShortInetDiagMsg(t *testing.T) {
	short := make([]byte, 40)
	buf := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, short)
	_, _, err := parseDiagMessages(buf)
	if err == nil {
		t.Fatal("expected error for short message")
	}
}

func TestParseNLError(t *testing.T) {
	payload := make([]byte, 4)
	eacces := int32(-13)
	binary.NativeEndian.PutUint32(payload, uint32(eacces))
	msg := buildTestNLMsg(unix.NLMSG_ERROR, payload)
	_, _, err := parseDiagMessages(msg)
	if err == nil {
		t.Fatal("expected error")
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) || errno != syscall.Errno(13) {
		t.Errorf("expected errno 13, got %v", err)
	}
}

func TestDumpTCPLive(t *testing.T) {
	conns, err := DumpTCP()
	if err != nil {
		t.Skipf("DumpTCP failed (possibly not supported): %v", err)
	}
	for _, c := range conns {
		if c.Family != unix.AF_INET && c.Family != unix.AF_INET6 {
			t.Errorf("unexpected family %d", c.Family)
		}
		t.Logf("state=%d local=%s remote=%s uid=%d info=%v rtt=%s", c.State, c.LocalAddr, c.RemoteAddr, c.UID, c.HasInfo, c.RTT)
	}
}

// kernelTCPInfo lays the fields the parser reads out as the kernel does,
// padding included, over the kernel's full struct size (232 bytes on 6.x).
func kernelTCPInfo(info *unix.TCPInfo) []byte {
	b := make([]byte, 232)
	b[0] = info.State
	binary.NativeEndian.PutUint32(b[tcpiRTT:], info.Rtt)
	binary.NativeEndian.PutUint32(b[tcpiTotalRetrans:], info.Total_retrans)
	binary.NativeEndian.PutUint64(b[tcpiBytesReceived:], info.Bytes_received)
	binary.NativeEndian.PutUint64(b[tcpiBytesSent:], info.Bytes_sent)
	return b
}

// The offsets must match the C layout of struct tcp_info, which unix.TCPInfo
// mirrors with natural alignment. A packed read (binary.Read) is two bytes
// off from tcpi_rto on, which multiplied every value by 65536.
func TestTCPInfoOffsetsMatchKernelLayout(t *testing.T) {
	var i unix.TCPInfo
	for _, tc := range []struct {
		name      string
		got, want uintptr
	}{
		{"rtt", tcpiRTT, unsafe.Offsetof(i.Rtt)},
		{"total_retrans", tcpiTotalRetrans, unsafe.Offsetof(i.Total_retrans)},
		{"bytes_received", tcpiBytesReceived, unsafe.Offsetof(i.Bytes_received)},
		{"bytes_sent", tcpiBytesSent, unsafe.Offsetof(i.Bytes_sent)},
	} {
		if tc.got != tc.want {
			t.Errorf("%s offset = %d, kernel layout = %d", tc.name, tc.got, tc.want)
		}
	}
	if packed := binary.Size(&i); uintptr(packed) == unsafe.Sizeof(i) {
		t.Logf("unix.TCPInfo has no implicit padding in this x/sys version")
	}
}

func TestParseTCPInfoAtKernelOffsets(t *testing.T) {
	info := unix.TCPInfo{Rtt: 151, Total_retrans: 2, Bytes_sent: 14892, Bytes_received: 9000}
	buf := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, buildInetDiagMsg(unix.AF_INET, 1, 40000, 443,
		netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), 0, 1, &info, 0))
	conns, _, err := parseDiagMessages(buf)
	if err != nil || len(conns) != 1 {
		t.Fatalf("parse: %v, %d conns", err, len(conns))
	}
	c := conns[0]
	if c.RTT != 151*time.Microsecond || c.Retransmits != 2 || c.BytesSent != 14892 || c.BytesReceived != 9000 {
		t.Errorf("got rtt=%v retrans=%d sent=%d recv=%d", c.RTT, c.Retransmits, c.BytesSent, c.BytesReceived)
	}
}

func TestParseOldKernelShortTCPInfo(t *testing.T) {
	info := unix.TCPInfo{Rtt: 300, Total_retrans: 1, Bytes_received: 50, Bytes_sent: 70}
	buf := buildTestNLMsg(unix.SOCK_DIAG_BY_FAMILY, buildInetDiagMsg(unix.AF_INET, 1, 40000, 443,
		netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), 0, 1, &info, 136))
	conns, _, err := parseDiagMessages(buf)
	if err != nil || len(conns) != 1 {
		t.Fatalf("parse: %v, %d conns", err, len(conns))
	}
	c := conns[0]
	if c.RTT != 300*time.Microsecond || c.Retransmits != 1 || c.BytesReceived != 50 || c.BytesSent != 0 {
		t.Errorf("got rtt=%v retrans=%d recv=%d sent=%d", c.RTT, c.Retransmits, c.BytesReceived, c.BytesSent)
	}
}

// A real loopback connection: the kernel's own counters for a known exchange
// must come back at their real size, not shifted.
func TestDumpTCPLiveValuesArePlausible(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = io.Copy(io.Discard, c)
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	const sent = 10000
	if _, err := client.Write(make([]byte, sent)); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	conns, err := DumpTCP()
	if err != nil {
		t.Skipf("DumpTCP failed (possibly not supported): %v", err)
	}
	local := netip.MustParseAddrPort(client.LocalAddr().String())
	for _, c := range conns {
		if c.LocalAddr != local || !c.HasInfo {
			continue
		}
		if c.BytesSent != 0 && (c.BytesSent < sent || c.BytesSent > sent*4) {
			t.Errorf("bytes_sent = %d for %d bytes written", c.BytesSent, sent)
		}
		if c.RTT <= 0 || c.RTT > time.Second {
			t.Errorf("loopback rtt = %v", c.RTT)
		}
		if c.Retransmits > 100 {
			t.Errorf("loopback retransmits = %d", c.Retransmits)
		}
		return
	}
	t.Skip("client socket not found in the dump")
}

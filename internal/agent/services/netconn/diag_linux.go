//go:build linux

package netconn

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// DumpTCP returns all TCP sockets for both IPv4 and IPv6 using sock_diag.
func DumpTCP() ([]Conn, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)

	var all []Conn
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		req := buildRequest(family)
		if err := unix.Send(fd, req, 0); err != nil {
			if family == unix.AF_INET6 && (err == unix.EAFNOSUPPORT || err == unix.EPROTONOSUPPORT) {
				continue
			}
			return nil, err
		}

		// Large enough for one dump datagram: a smaller buffer truncates it.
		buf := make([]byte, 64<<10)
		for {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				return nil, err
			}
			msgs, done, err := parseDiagMessages(buf[:n])
			if err != nil {
				// A kernel without IPv6 answers the AF_INET6 dump with an error.
				if family == unix.AF_INET6 && (err == unix.EAFNOSUPPORT || err == unix.ENOENT || err == unix.EINVAL) {
					break
				}
				return nil, err
			}
			all = append(all, msgs...)
			if done {
				break
			}
		}
	}
	return all, nil
}

// buildRequest constructs a netlink message for an inet_diag dump request.
func buildRequest(family uint8) []byte {
	// inet_diag_req_v2 layout (56 bytes):
	//   sdiag_family   u8
	//   sdiag_protocol u8
	//   idiag_ext      u8
	//   pad            u8
	//   idiag_states   u32
	//   id             inet_diag_sockid (48 bytes)
	const reqLen = 56
	payload := make([]byte, reqLen)
	payload[0] = family
	payload[1] = unix.IPPROTO_TCP
	payload[2] = 1 << (2 - 1) // INET_DIAG_INFO = 2
	binary.NativeEndian.PutUint32(payload[4:8], 0xffffffff)

	// Netlink header
	hdr := unix.NlMsghdr{
		Len:   uint32(syscall.NLMSG_HDRLEN + reqLen),
		Type:  unix.SOCK_DIAG_BY_FAMILY,
		Flags: unix.NLM_F_REQUEST | unix.NLM_F_DUMP,
		Seq:   1,
		Pid:   0,
	}
	buf := make([]byte, int(hdr.Len))
	binary.NativeEndian.PutUint32(buf[0:4], hdr.Len)
	binary.NativeEndian.PutUint16(buf[4:6], hdr.Type)
	binary.NativeEndian.PutUint16(buf[6:8], hdr.Flags)
	binary.NativeEndian.PutUint32(buf[8:12], hdr.Seq)
	binary.NativeEndian.PutUint32(buf[12:16], hdr.Pid)
	copy(buf[16:], payload)
	return buf
}

// parseDiagMessages interprets a buffer of netlink messages.
// It returns the parsed connections, whether NLMSG_DONE was seen, and an error.
func parseDiagMessages(b []byte) (conns []Conn, done bool, err error) {
	msgs, err := syscall.ParseNetlinkMessage(b)
	if err != nil {
		return nil, false, err
	}
	for _, msg := range msgs {
		switch msg.Header.Type {
		case unix.NLMSG_DONE:
			return conns, true, nil
		case unix.NLMSG_ERROR:
			if len(msg.Data) < 4 {
				return nil, false, fmt.Errorf("short NLMSG_ERROR")
			}
			errno := int32(binary.NativeEndian.Uint32(msg.Data[:4]))
			if errno != 0 {
				return nil, false, syscall.Errno(-errno)
			}
		case unix.SOCK_DIAG_BY_FAMILY:
			c, err := parseInetDiagMsg(msg.Data)
			if err != nil {
				return nil, false, err
			}
			conns = append(conns, c)
		}
	}
	return conns, false, nil
}

// parseInetDiagMsg parses an inet_diag_msg and its attributes.
func parseInetDiagMsg(data []byte) (Conn, error) {
	var c Conn
	if len(data) < 72 {
		return c, fmt.Errorf("inet_diag_msg too short: %d bytes", len(data))
	}

	c.Family = data[0]
	c.State = data[1]
	// data[2] = timer, data[3] = retrans (kernel retrans count, not used)

	sport := binary.BigEndian.Uint16(data[4:6])
	dport := binary.BigEndian.Uint16(data[6:8])

	switch c.Family {
	case unix.AF_INET:
		var src4 [4]byte
		copy(src4[:], data[8:12])
		localIP := netip.AddrFrom4(src4)
		var dst4 [4]byte
		copy(dst4[:], data[24:28])
		remoteIP := netip.AddrFrom4(dst4)
		c.LocalAddr = netip.AddrPortFrom(localIP, sport)
		c.RemoteAddr = netip.AddrPortFrom(remoteIP, dport)
	case unix.AF_INET6:
		var src16 [16]byte
		copy(src16[:], data[8:24])
		localIP := netip.AddrFrom16(src16)
		var dst16 [16]byte
		copy(dst16[:], data[24:40])
		remoteIP := netip.AddrFrom16(dst16)
		c.LocalAddr = netip.AddrPortFrom(localIP, sport)
		c.RemoteAddr = netip.AddrPortFrom(remoteIP, dport)
	default:
		return c, fmt.Errorf("unsupported address family %d", c.Family)
	}

	// Native endian fields after the sockid
	c.UID = binary.NativeEndian.Uint32(data[64:68])
	c.Inode = binary.NativeEndian.Uint32(data[68:72])

	// Parse attributes
	if len(data) > 72 {
		attrs := data[72:]
		for len(attrs) >= 4 {
			rtaLen := int(binary.NativeEndian.Uint16(attrs[0:2]))
			rtaType := binary.NativeEndian.Uint16(attrs[2:4])
			if rtaLen < 4 || rtaLen > len(attrs) {
				return c, fmt.Errorf("bad rtattr length %d", rtaLen)
			}
			payload := attrs[4:rtaLen]
			if rtaType == 2 { // INET_DIAG_INFO
				c.HasInfo = true
				if v, ok := tcpInfoU32(payload, tcpiRTT); ok {
					c.RTT = time.Duration(v) * time.Microsecond
				}
				if v, ok := tcpInfoU32(payload, tcpiTotalRetrans); ok {
					c.Retransmits = v
				}
				if v, ok := tcpInfoU64(payload, tcpiBytesReceived); ok {
					c.BytesReceived = v
				}
				if v, ok := tcpInfoU64(payload, tcpiBytesSent); ok {
					c.BytesSent = v
				}
				break
			}
			// Align to 4 bytes
			aligned := (rtaLen + 3) &^ 3
			if aligned > len(attrs) {
				aligned = len(attrs)
			}
			attrs = attrs[aligned:]
		}
	}
	return c, nil
}

// Offsets of struct tcp_info fields as the kernel lays them out
// (include/uapi/linux/tcp.h). binary.Read over unix.TCPInfo cannot be used:
// it packs the Go struct and drops the two bytes of padding the kernel keeps
// before tcpi_rto, which shifts every later field by two bytes. An older
// kernel sends a shorter struct; a field beyond its end stays zero.
const (
	tcpiRTT           = 68
	tcpiTotalRetrans  = 100
	tcpiBytesReceived = 128
	tcpiBytesSent     = 200
)

func tcpInfoU32(b []byte, off int) (uint32, bool) {
	if len(b) < off+4 {
		return 0, false
	}
	return binary.NativeEndian.Uint32(b[off : off+4]), true
}

func tcpInfoU64(b []byte, off int) (uint64, bool) {
	if len(b) < off+8 {
		return 0, false
	}
	return binary.NativeEndian.Uint64(b[off : off+8]), true
}

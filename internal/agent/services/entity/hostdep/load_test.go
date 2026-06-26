//go:build loadtest

// Load harness for #492: measure the per-scrape cost of the hostdep source on a
// host carrying many ESTABLISHED sockets. The dominant cost is gnet.Connections
// ("tcp") — on Linux it parses /proc/net/tcp{,6} and builds the inode->pid map
// from /proc/*/fd, so it scales with the host's total sockets and fds, not just
// the agent's own. This harness stands up N real connections in-process (to a
// non-loopback host IP, so the full classify+name path is exercised) and times
// both gnet.Connections alone and a full Observe.
//
// The per-pid name resolution is bounded by a cross-scrape (pid, createTime)
// LRU (#492): after the first scrape a stable owning process is served from the
// cache, so BenchmarkObserveUnderLoad past the first iteration reflects the
// steady-state cost (socket read + classify), not repeated process.Name calls.
//
// Excluded from normal builds/CI (build tag `loadtest`). Run on a real Linux
// host:
//
//	go test -c -tags loadtest -o /tmp/hostdep.load ./internal/agent/services/entity/hostdep
//	scp /tmp/hostdep.load <host>:/tmp/ && ssh <host> '/tmp/hostdep.load -test.bench=. -test.benchmem -test.benchtime=20x'
package hostdep

import (
	"fmt"
	"net"
	"testing"

	gnet "github.com/shirou/gopsutil/v3/net"
)

// primaryIPv4 returns the host's first non-loopback IPv4, so the synthesized
// peers are resolvable (loopback peers are skipped by scrape, which would hide
// the per-PID name-resolution cost).
func primaryIPv4(tb testing.TB) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		tb.Fatalf("interfaces: %v", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.To4() == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	tb.Skip("no non-loopback IPv4 interface")
	return ""
}

// establishN opens n client connections to an in-process listener bound on the
// host's primary IP, returning a cleanup that closes everything.
func establishN(tb testing.TB, n int) func() {
	ip := primaryIPv4(tb)
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		tb.Fatalf("listen: %v", err)
	}
	accepted := make([]net.Conn, 0, n)
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(done)
				return
			}
			accepted = append(accepted, c)
		}
	}()
	clients := make([]net.Conn, 0, n)
	for i := 0; i < n; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			tb.Fatalf("dial %d/%d: %v", i, n, err)
		}
		clients = append(clients, c)
	}
	return func() {
		for _, c := range clients {
			_ = c.Close()
		}
		_ = ln.Close()
		<-done
		for _, c := range accepted {
			_ = c.Close()
		}
	}
}

func BenchmarkGnetConnectionsUnderLoad(b *testing.B) {
	for _, n := range []int{0, 100, 1000, 5000, 10000} {
		cleanup := establishN(b, n)
		b.Run(fmt.Sprintf("conns=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := gnet.Connections("tcp"); err != nil {
					b.Fatal(err)
				}
			}
		})
		cleanup()
	}
}

func BenchmarkObserveUnderLoad(b *testing.B) {
	for _, n := range []int{0, 100, 1000, 5000, 10000} {
		cleanup := establishN(b, n)
		b.Run(fmt.Sprintf("conns=%d", n), func(b *testing.B) {
			s := New(func() string { return "load-host" }, defaultThreshold, nil)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, ok := s.Observe(); !ok {
					b.Fatal("observe ok=false")
				}
			}
		})
		cleanup()
	}
}

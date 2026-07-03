package http

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// errConnectivityTargetBlocked is returned when a connectivity test resolves to
// an address the agent must not reach. Callers surface it as the test error, so
// the reason is distinguishable from an ordinary "host unreachable".
const errConnectivityTargetBlocked = "connectivity target blocked: link-local/metadata address is not a valid probe target"

// blockedConnectivityIP reports whether ip must not be dialed by the config
// connectivity test. A monitoring agent legitimately probes loopback and
// private (RFC1918 / unique-local) hosts, so those stay allowed. The guard
// blocks link-local space (IPv4 169.254.0.0/16, IPv6 fe80::/10) because that is
// where cloud instance-metadata services live (169.254.169.254 on AWS, GCP,
// Azure, OpenStack, Alibaba) — never a legitimate probe endpoint, and the prime
// SSRF target for credential theft.
func blockedConnectivityIP(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// newConnectivityClient builds the HTTP client used by the /config/test
// connectivity checks. It differs from a plain client in two ways that matter
// for SSRF safety:
//
//   - a Dialer Control hook rejects link-local/metadata addresses at connect
//     time, i.e. after DNS resolution, so a hostname that resolves (or rebinds)
//     to 169.254.169.254 is caught on the concrete IP; and
//   - redirects are not followed, so an allowed target cannot 30x-bounce the
//     request onto a blocked address.
func newConnectivityClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && blockedConnectivityIP(ip) {
				return fmt.Errorf("%s (%s)", errConnectivityTargetBlocked, host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("%s (redirect not followed)", errConnectivityTargetBlocked)
		},
	}
}

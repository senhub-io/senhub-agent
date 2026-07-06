package http

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// errConnectivityTargetBlocked is returned when a connectivity test resolves to
// an address the agent must not reach. Callers surface it as the test error, so
// the reason is distinguishable from an ordinary "host unreachable".
const errConnectivityTargetBlocked = "connectivity target blocked: link-local/metadata address is not a valid probe target"

// extraBlockedMetadataIPs holds cloud instance-metadata endpoints that live
// OUTSIDE link-local space and so slip past the IsLinkLocal* checks:
//   - fd00:ec2::254  — AWS IMDS over IPv6 (unique-local fd00::/8, NOT link-local)
//   - 168.63.129.16  — Azure WireServer (public-range, a documented SSRF pivot
//     for extension protected-settings)
//
// Both are documented constants and never a legitimate probe target.
var extraBlockedMetadataIPs = []net.IP{
	net.ParseIP("fd00:ec2::254"),
	net.ParseIP("168.63.129.16"),
}

// blockedConnectivityIP reports whether ip must not be dialed by the config
// connectivity test. A monitoring agent legitimately probes loopback and
// private (RFC1918 / unique-local) hosts, so those stay allowed. The guard
// blocks link-local space (IPv4 169.254.0.0/16, IPv6 fe80::/10) — where the
// classic metadata service lives (169.254.169.254 on AWS, GCP, Azure,
// OpenStack, Alibaba) — plus the non-link-local metadata endpoints in
// extraBlockedMetadataIPs. These are the prime SSRF targets for credential
// theft, never a legitimate probe endpoint.
func blockedConnectivityIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, blocked := range extraBlockedMetadataIPs {
		if blocked != nil && ip.Equal(blocked) {
			return true
		}
	}
	return false
}

// parseHostIP extracts the concrete IP from a dial host, stripping an IPv6 zone
// suffix (%zone) first. net.ParseIP rejects zoned literals like "fe80::1%eth0",
// which would otherwise yield nil and bypass the block list entirely.
func parseHostIP(host string) net.IP {
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host)
}

// newConnectivityClient builds the HTTP client used by the /config/test
// connectivity checks. It differs from a plain client in two ways that matter
// for SSRF safety:
//
//   - a Dialer Control hook rejects link-local/metadata addresses at connect
//     time, i.e. after DNS resolution, so a hostname that resolves (or rebinds)
//     to 169.254.169.254 is caught on the concrete IP; and
//   - redirects are surfaced, not followed: the client returns the 3xx response
//     itself (http.ErrUseLastResponse) instead of chasing the Location. The
//     target stays reported as reachable (a http→https 301 is legitimate and
//     near-universal), while the redirect destination is never dialed — and if
//     it were, the same Control hook re-checks every hop's concrete IP.
func newConnectivityClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := parseHostIP(host); ip != nil && blockedConnectivityIP(ip) {
				return fmt.Errorf("%s (%s)", errConnectivityTargetBlocked, host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

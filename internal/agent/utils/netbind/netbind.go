// Package netbind classifies listener bind addresses. All agent
// listeners default to loopback since #278; binding all interfaces is
// an explicit operator opt-in that callers surface with a startup
// warning, keyed off IsWildcard.
package netbind

import (
	"net"
	"strings"
)

// IsWildcard reports whether addr binds all interfaces. It accepts a
// bare host ("0.0.0.0", "::"), a host:port ("0.0.0.0:162",
// "[::]:4317"), or a host:port with an empty host (":8080"), which
// the net package also treats as bind-all.
func IsWildcard(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		if h == "" {
			return true
		}
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

package entity

import "testing"

func TestCanonicalCIDR(t *testing.T) {
	cases := []struct {
		name   string
		ip     string
		prefix int
		want   string
		ok     bool
	}{
		{"v4 host bits zeroed", "10.20.3.7", 24, "10.20.3.0/24", true},
		{"v4 already masked", "10.20.3.0", 24, "10.20.3.0/24", true},
		{"v4 /32 explicit", "192.0.2.1", 32, "192.0.2.1/32", true},
		{"v4 default route", "0.0.0.0", 0, "0.0.0.0/0", true},
		{"v6 default route", "::", 0, "::/0", true},
		{"v6 /128 explicit", "2001:db8::1", 128, "2001:db8::1/128", true},
		{"v6 host bits zeroed", "2001:db8::dead:beef", 64, "2001:db8::/64", true},
		{"v6 RFC 5952 lowercase + compression", "2001:0DB8:0000:0000:0000:0000:0000:0001", 128, "2001:db8::1/128", true},
		{"v4-mapped v6 unmapped", "::ffff:10.20.3.7", 24, "10.20.3.0/24", true},
		{"zone stripped", "fe80::1%eth0", 64, "fe80::/64", true},
		{"surrounding space trimmed", " 10.0.0.0 ", 8, "10.0.0.0/8", true},
		{"v4 prefix too long", "10.0.0.0", 33, "", false},
		{"v6 prefix too long", "2001:db8::", 129, "", false},
		{"negative prefix", "10.0.0.0", -1, "", false},
		{"not an ip", "not-an-ip", 24, "", false},
		{"empty", "", 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CanonicalCIDR(c.ip, c.prefix)
			if got != c.want || ok != c.ok {
				t.Errorf("CanonicalCIDR(%q, %d) = %q, %v; want %q, %v", c.ip, c.prefix, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestCanonicalIP(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"v4", "10.0.0.1", "10.0.0.1", true},
		{"v6 lowercased", "2001:DB8::1", "2001:db8::1", true},
		{"v6 compressed once", "2001:0db8:0000:0000:0001:0000:0000:0001", "2001:db8::1:0:0:1", true},
		{"v4-mapped unmapped", "::ffff:192.0.2.7", "192.0.2.7", true},
		{"zone kept verbatim", "fe80::1%eth0", "fe80::1%eth0", true},
		{"surrounding space trimmed", " ::1 ", "::1", true},
		{"hostname rejected", "core-sw.example", "", false},
		{"empty rejected", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := CanonicalIP(c.in)
			if got != c.want || ok != c.ok {
				t.Errorf("CanonicalIP(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

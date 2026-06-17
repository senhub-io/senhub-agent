package snmppoll

import (
	"net"
	"testing"
)

func cidr(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("bad cidr %q: %v", s, err)
	}
	return n
}

func deviceIDs(ds []discoveredDevice) map[string]bool {
	m := map[string]bool{}
	for _, d := range ds {
		m[d.ID] = true
	}
	return m
}

func TestCrawl_BoundedBFS(t *testing.T) {
	// Ring sw-a↔sw-b↔sw-c; sw-c also peers 8.8.8.8 (outside allowed).
	fabric := map[string]struct {
		id    string
		neigh []string
	}{
		"10.0.0.1": {"dev-a", []string{"10.0.0.2"}},
		"10.0.0.2": {"dev-b", []string{"10.0.0.1", "10.0.0.3"}},
		"10.0.0.3": {"dev-c", []string{"10.0.0.2", "8.8.8.8"}},
		"8.8.8.8":  {"dev-evil", nil}, // outside allowed → must never be polled
	}
	poll := func(ip string) (string, []string) {
		d, ok := fabric[ip]
		if !ok {
			return "", nil
		}
		return d.id, d.neigh
	}
	out := crawl([]string{"10.0.0.1"}, poll,
		crawlBounds{MaxDevices: 100, MaxHops: 10, Allowed: []*net.IPNet{cidr(t, "10.0.0.0/24")}})

	ids := deviceIDs(out)
	if len(out) != 3 || !ids["dev-a"] || !ids["dev-b"] || !ids["dev-c"] {
		t.Fatalf("crawl = %+v, want dev-a/b/c", out)
	}
	if ids["dev-evil"] {
		t.Error("8.8.8.8 is outside allowed_cidrs — must not be crawled")
	}
}

func TestCrawl_DedupByDeviceID(t *testing.T) {
	// Same device answers on two management IPs → one node.
	poll := func(ip string) (string, []string) {
		switch ip {
		case "10.0.0.1":
			return "dev-x", []string{"10.0.0.9"}
		case "10.0.0.9":
			return "dev-x", nil // same device, second management IP
		}
		return "", nil
	}
	out := crawl([]string{"10.0.0.1"}, poll,
		crawlBounds{MaxDevices: 100, MaxHops: 5, Allowed: []*net.IPNet{cidr(t, "10.0.0.0/24")}})
	if len(out) != 1 || out[0].ID != "dev-x" {
		t.Fatalf("crawl = %+v, want one dev-x (deduped by device.id)", out)
	}
}

func TestCrawl_MaxHops(t *testing.T) {
	// Linear chain a→b→c; max_hops=1 → only seed (hop 0) + direct neighbour.
	poll := func(ip string) (string, []string) {
		switch ip {
		case "10.0.0.1":
			return "a", []string{"10.0.0.2"}
		case "10.0.0.2":
			return "b", []string{"10.0.0.3"}
		case "10.0.0.3":
			return "c", nil
		}
		return "", nil
	}
	out := crawl([]string{"10.0.0.1"}, poll,
		crawlBounds{MaxDevices: 100, MaxHops: 1, Allowed: []*net.IPNet{cidr(t, "10.0.0.0/24")}})
	if len(out) != 2 {
		t.Fatalf("max_hops=1 → %d devices (%+v), want 2 (a + b)", len(out), out)
	}
}

func TestCrawl_MaxDevices(t *testing.T) {
	// Every device points to the next; cap at 2.
	poll := func(ip string) (string, []string) {
		switch ip {
		case "10.0.0.1":
			return "a", []string{"10.0.0.2"}
		case "10.0.0.2":
			return "b", []string{"10.0.0.3"}
		case "10.0.0.3":
			return "c", nil
		}
		return "", nil
	}
	out := crawl([]string{"10.0.0.1"}, poll,
		crawlBounds{MaxDevices: 2, MaxHops: 10, Allowed: []*net.IPNet{cidr(t, "10.0.0.0/24")}})
	if len(out) != 2 {
		t.Fatalf("max_devices=2 → %d devices, want 2", len(out))
	}
}

func TestCrawl_EmptyAllowedStaysAtSeeds(t *testing.T) {
	poll := func(ip string) (string, []string) {
		switch ip {
		case "10.0.0.1":
			return "seed", []string{"10.0.0.2"}
		case "10.0.0.2":
			return "neighbour", nil
		}
		return "", nil
	}
	out := crawl([]string{"10.0.0.1"}, poll,
		crawlBounds{MaxDevices: 100, MaxHops: 5, Allowed: nil})
	if len(out) != 1 || out[0].ID != "seed" {
		t.Fatalf("empty allowed_cidrs → only seeds, got %+v", out)
	}
}

func TestCrawl_SeedNoAnswerSkipped(t *testing.T) {
	poll := func(ip string) (string, []string) { return "", nil } // nothing answers
	out := crawl([]string{"10.0.0.1", "10.0.0.2"}, poll,
		crawlBounds{MaxDevices: 100, MaxHops: 5, Allowed: []*net.IPNet{cidr(t, "10.0.0.0/24")}})
	if len(out) != 0 {
		t.Fatalf("no device answers → %+v, want empty", out)
	}
}

package entity

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"127.0.1.1", true},
		{"::1", true},
		{"10.0.0.5", false},
		{"192.168.1.10", false},
		{"db.internal", false},
		{"pub400.com", false},
	}
	for _, c := range cases {
		if got := IsLoopbackHost(c.addr); got != c.want {
			t.Errorf("IsLoopbackHost(%q)=%v want %v", c.addr, got, c.want)
		}
	}
}

func TestLocalRunsOn(t *testing.T) {
	svcID := map[string]any{"service.instance.id": "nginx@H"}

	rel, ok := LocalRunsOn("service.instance", svcID, "127.0.0.1", "H")
	if !ok {
		t.Fatal("loopback target with host id: expected a runs_on relation")
	}
	if rel.Type != "runs_on" || rel.FromType != "service.instance" || rel.ToType != "host" {
		t.Errorf("unexpected relation shape: %+v", rel)
	}
	if rel.ToID["host.id"] != "H" {
		t.Errorf("runs_on target host.id = %v, want H", rel.ToID["host.id"])
	}

	if _, ok := LocalRunsOn("service.instance", svcID, "10.0.0.5", "H"); ok {
		t.Error("remote target must NOT produce a runs_on")
	}
	if _, ok := LocalRunsOn("service.instance", svcID, "127.0.0.1", ""); ok {
		t.Error("empty host id must NOT produce a runs_on")
	}
}

// TestLocalRunsOn_CollapseGuard: a network-derived identity (one that embeds the
// loopback address) is identical on every host, so anchoring it to a host would
// false-join all hosts. The helper must refuse it.
func TestLocalRunsOn_CollapseGuard(t *testing.T) {
	// Loopback literal embedded in the identity → refused (false-join guard).
	if _, ok := LocalRunsOn("service.instance", map[string]any{"service.instance.id": "modbus://127.0.0.1:502"}, "127.0.0.1", "H"); ok {
		t.Error("network-derived id embedding 127.0.0.1 must be refused (collapse guard)")
	}
	if _, ok := LocalRunsOn("db", map[string]any{"db.instance.id": "mssql:localhost:1433"}, "localhost", "H"); ok {
		t.Error("network-derived id embedding localhost must be refused (collapse guard)")
	}
	// Host-scoped and tech ids do NOT embed the loopback literal → allowed.
	if _, ok := LocalRunsOn("service.instance", map[string]any{"service.instance.id": "nginx@H"}, "127.0.0.1", "H"); !ok {
		t.Error("host-scoped id must be allowed on loopback")
	}
	if _, ok := LocalRunsOn("db", map[string]any{"db.instance.id": "postgresql:6543210987654321"}, "localhost", "H"); !ok {
		t.Error("tech id must be allowed on loopback")
	}
}

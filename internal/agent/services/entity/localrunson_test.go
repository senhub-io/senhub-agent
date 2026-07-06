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

// TestLocalRunsOn_CollapseGuardCrossForm: the loopback address has interchangeable
// spellings; an id minted from one form while the target is passed in another must
// still be refused (localhost vs 127.0.0.1 vs ::1 are one equivalence class).
func TestLocalRunsOn_CollapseGuardCrossForm(t *testing.T) {
	cases := []struct {
		name, idValue, serverAddress string
	}{
		{"id localhost, target 127.0.0.1", "name:localhost", "127.0.0.1"},
		{"id 127.0.0.1, target localhost", "mgmt:127.0.0.1", "localhost"},
		{"id ::1, target 127.0.0.1", "name:node-::1", "127.0.0.1"},
		{"id localhost, target ::1", "name:localhost", "::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := map[string]any{"network.device.id": c.idValue}
			if _, ok := LocalRunsOn("network.device", id, c.serverAddress, "H"); ok {
				t.Errorf("cross-form loopback id %q with target %q must be refused", c.idValue, c.serverAddress)
			}
		})
	}
}

package dbcommon

import "testing"

func TestLocalHostRunsOn(t *testing.T) {
	dbID := map[string]any{"db.instance.id": "mysql:uuid"}

	cases := []struct {
		name, addr, hostID string
		wantOK             bool
	}{
		{"loopback ipv4", "127.0.0.1", "h-1", true},
		{"loopback ipv6", "::1", "h-1", true},
		{"localhost", "localhost", "h-1", true},
		{"empty (unix socket / default)", "", "h-1", true},
		{"remote ip", "10.0.0.5", "h-1", false},
		{"remote host", "db.example.com", "h-1", false},
		{"loopback but no host id", "127.0.0.1", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, ok := LocalHostRunsOn(dbID, c.addr, c.hostID)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if rel.Type != "runs_on" || rel.FromType != "db" || rel.ToType != "host" {
				t.Errorf("relation shape wrong: %+v", rel)
			}
			if rel.FromID["db.instance.id"] != "mysql:uuid" {
				t.Errorf("FromID must carry the db identity: %+v", rel.FromID)
			}
			if rel.ToID["host.id"] != c.hostID {
				t.Errorf("ToID host.id = %v, want %v", rel.ToID["host.id"], c.hostID)
			}
		})
	}
}

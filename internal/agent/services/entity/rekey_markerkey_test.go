package entity

import "testing"

// TestRekeyMarkerKeyKeepsTheSingleKeySpelling pins the marker name of the
// re-keys announced before multi-key ids, so hosts do not announce them again.
func TestRekeyMarkerKeyKeepsTheSingleKeySpelling(t *testing.T) {
	r := NewRekeyAnnouncer("db", "db.instance.id", "old", "new")
	if got, want := r.markerKey(), "db|db.instance.id|old|new"; got != want {
		t.Errorf("markerKey = %q, want %q", got, want)
	}
	typed := NewRekeyAnnouncerWithIDs("service.instance", map[string]any{"service.instance.id": "systemd://h/a.service"},
		"host.service", map[string]any{"host.id": "h", "host.service.name": "a.service"})
	if typed.markerKey() == r.markerKey() || typed.markerKey() == "" {
		t.Errorf("typed markerKey = %q", typed.markerKey())
	}
}

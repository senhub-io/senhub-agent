package entity

import "testing"

func TestRekeyAnnouncer_AliasPrecedesTheDelete(t *testing.T) {
	EnableRekeyMarkers("", nil)
	a := NewRekeyAnnouncer("service.instance", "service.instance.id", "old", "new")

	if n := countRekeyDeletes(t, func() { a.Announce() }); n != 0 {
		t.Fatalf("first cycle: got %d deletes, want 0", n)
	}
	if _, ok := a.SameAs(); !ok {
		t.Fatal("first cycle: same_as missing, want present")
	}

	for cycle := 2; cycle <= 4; cycle++ {
		if n := countRekeyDeletes(t, func() { a.Announce() }); n != 1 {
			t.Fatalf("cycle %d: got %d deletes, want 1", cycle, n)
		}
		if _, ok := a.SameAs(); !ok {
			t.Fatalf("cycle %d: same_as missing, want present", cycle)
		}
	}

	if n := countRekeyDeletes(t, func() { a.Announce() }); n != 0 {
		t.Fatalf("cycle 5: got %d deletes, want 0", n)
	}
	if _, ok := a.SameAs(); ok {
		t.Fatal("cycle 5: same_as present, want absent")
	}
}

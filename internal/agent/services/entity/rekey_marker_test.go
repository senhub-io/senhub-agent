package entity

import (
	"os"
	"path/filepath"
	"testing"
)

func countRekeyDeletes(t *testing.T, run func()) int {
	t.Helper()
	ResetForTest()
	ch := SubscribeEvents(64)
	run()
	n := 0
	for {
		select {
		case ev := <-ch:
			if ev.Kind == EntityDelete {
				n++
			}
		default:
			return n
		}
	}
}

func announceCycles(legacy, id string) func() {
	return func() {
		a := NewRekeyAnnouncer("service.instance", "service.instance.id", legacy, id)
		for i := 0; i < RekeyAnnounceCycles+3; i++ {
			a.Announce()
		}
	}
}

func TestRekeyMarker_FirstStartAnnouncesSecondDoesNot(t *testing.T) {
	dir := t.TempDir()
	EnableRekeyMarkers(dir, nil)
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })

	if got := countRekeyDeletes(t, announceCycles("old", "new")); got != RekeyAnnounceCycles {
		t.Fatalf("first start: %d deletes, want %d", got, RekeyAnnounceCycles)
	}
	if got := countRekeyDeletes(t, announceCycles("old", "new")); got != 0 {
		t.Fatalf("second start: %d deletes, want 0", got)
	}
	a := NewRekeyAnnouncer("service.instance", "service.instance.id", "old", "new")
	if _, ok := a.SameAs(); ok {
		t.Error("second start must not draw the alias again")
	}
	if got := countRekeyDeletes(t, announceCycles("old2", "new2")); got != RekeyAnnounceCycles {
		t.Fatalf("a different re-key must still announce: %d", got)
	}
}

func TestRekeyMarker_UnwritableAnnouncesAndWarnsOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	warned := 0
	EnableRekeyMarkers(filepath.Join(file, "state"), func(error) { warned++ })
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })

	for i := 0; i < 2; i++ {
		if got := countRekeyDeletes(t, announceCycles("old", "new")); got != RekeyAnnounceCycles {
			t.Fatalf("start %d: %d deletes, want %d", i, got, RekeyAnnounceCycles)
		}
	}
	if warned != 1 {
		t.Errorf("warned %d times, want once", warned)
	}
}

func TestRekeyMarker_DisabledKeepsStatelessBehaviour(t *testing.T) {
	EnableRekeyMarkers("", nil)
	for i := 0; i < 2; i++ {
		if got := countRekeyDeletes(t, announceCycles("old", "new")); got != RekeyAnnounceCycles {
			t.Fatalf("start %d: %d deletes", i, got)
		}
	}
}

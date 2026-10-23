package hostpoll

import (
	"testing"
	"time"
)

func TestHandoverBase(t *testing.T) {
	t0 := time.Unix(1780272000, 0)
	var h Handover[int]
	if _, _, ok := h.Base(t0); ok {
		t.Fatal("an empty handover gave a base")
	}

	h.Store(1, t0)
	h.Store(2, t0.Add(30*time.Second))

	if v, _, _ := h.Base(t0.Add(40 * time.Second)); v != 2 {
		t.Errorf("10 s after the newest reading: base %d, want the newest (2)", v)
	}
	if v, _, _ := h.Base(t0.Add(32 * time.Second)); v != 1 {
		t.Errorf("2 s after the newest reading: base %d, want the older one (1), 32 s old", v)
	}
	if v, at, ok := h.Base(t0.Add(31 * time.Second)); !ok || v != 1 || !at.Equal(t0) {
		t.Errorf("base = %d at %s ok=%v, want 1 at %s", v, at, ok, t0)
	}

	var single Handover[int]
	single.Store(7, t0)
	if v, _, ok := single.Base(t0.Add(time.Second)); !ok || v != 7 {
		t.Errorf("a single recent reading: base %d ok=%v, want 7, to wait from", v, ok)
	}
	if _, _, ok := single.Base(t0.Add(maxHandoverAge + time.Second)); ok {
		t.Error("a reading older than maxHandoverAge was given as a base")
	}
	if _, _, ok := single.Base(t0.Add(-time.Second)); ok {
		t.Error("a reading from the future was given as a base")
	}
}

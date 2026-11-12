package auto_update

import "testing"

func TestSetPaused(t *testing.T) {
	defer SetPaused(false)
	if Paused() {
		t.Fatal("Paused() = true before any SetPaused call")
	}
	SetPaused(true)
	if !Paused() {
		t.Fatal("Paused() = false after SetPaused(true)")
	}
	SetPaused(false)
	if Paused() {
		t.Fatal("Paused() = true after SetPaused(false)")
	}
}

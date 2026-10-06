package state

import (
	"path/filepath"
	"testing"
)

func TestDir_EnvOverrideWins(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", "/run/unit-state")
	t.Setenv(EnvDir, "/data/state")
	if got := Dir(); got != "/data/state" {
		t.Fatalf("Dir() = %q", got)
	}
}

func TestDir_ServiceManagerThenDefault(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv("STATE_DIRECTORY", "/var/lib/unit-state:/var/lib/other")
	if got := Dir(); got != "/var/lib/unit-state" {
		t.Fatalf("Dir() = %q", got)
	}
	t.Setenv("STATE_DIRECTORY", "")
	if Dir() == "" {
		t.Fatal("no platform default")
	}
}

func TestPath_StaysInsideTheDirectory(t *testing.T) {
	t.Setenv(EnvDir, "/data/state")
	if got, want := Path("oltp.bookmark"), filepath.Join("/data/state", "oltp.bookmark"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if got, want := Path("../../etc/x"), filepath.Join("/data/state", "x"); got != want {
		t.Fatalf("a traversing name must not leave the directory: %q, want %q", got, want)
	}
}

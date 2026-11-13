//go:build linux

package process

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeStat(t *testing.T, root string, pid int32, line string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(int(pid)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsKernelThread(t *testing.T) {
	root := t.TempDir()
	// flags 0x00208040 has PF_KTHREAD; 4194560 (0x00400100) does not.
	writeStat(t, root, 33, "33 (scsi_eh_0) S 2 0 0 0 -1 2129984 0 0 0 0 0 0 0 0 20 0 1 0 5 0 0\n")
	writeStat(t, root, 900, "900 (my ) weird) app) S 1 900 900 0 -1 4194560 100 0 0 0 5 0 0 0 20 0 1 0 100 0 0\n")
	writeStat(t, root, 901, "901 (child) S 2 901 901 0 -1 notanumber 0\n")
	writeStat(t, root, 902, "902 (bash) S 1 902 902 0 -1 notanumber 0\n")

	cases := []struct {
		pid  int32
		want bool
		why  string
	}{
		{2, true, "kthreadd itself"},
		{33, true, "PF_KTHREAD set"},
		{900, false, "user process whose comm has spaces and parentheses"},
		{901, true, "unreadable flags, parent kthreadd"},
		{902, false, "unreadable flags, parent init"},
		{12345, false, "no stat file"},
	}
	for _, c := range cases {
		if got := isKernelThread(c.pid, root); got != c.want {
			t.Errorf("pid %d (%s): isKernelThread = %v, want %v", c.pid, c.why, got, c.want)
		}
	}
}

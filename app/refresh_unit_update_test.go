package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitRefresherAfterUpdate(t *testing.T) {
	const etcUnit = "/etc/systemd/system/senhub-agent.service"
	tests := []struct {
		name     string
		path     string
		before   string
		missing  bool
		after    string
		runErr   error
		wantRun  bool
		wantLine bool
		wantErr  bool
	}{
		{name: "older unit is refreshed", path: etcUnit, before: "old", after: "new", wantRun: true, wantLine: true},
		{name: "unit already current", path: etcUnit, before: "same", after: "same", wantRun: true},
		{name: "no unit installed", path: etcUnit, missing: true},
		{name: "package unit is left to the package", path: "/usr/lib/systemd/system/senhub-agent.service", before: "old", after: "new"},
		{name: "refresh-unit fails", path: etcUnit, before: "old", after: "old", runErr: errors.New("exit status 2"), wantRun: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			current := tt.before
			r := unitRefresher{
				unitPath: func() string { return tt.path },
				read: func(string) ([]byte, error) {
					if tt.missing {
						return nil, fs.ErrNotExist
					}
					return []byte(current), nil
				},
				run: func(bin string) ([]byte, error) {
					ran = true
					if bin != "/usr/local/bin/senhub-agent" {
						t.Errorf("refresh-unit ran from %q, want the new binary", bin)
					}
					if tt.runErr != nil {
						return []byte("writing unit file: permission denied"), tt.runErr
					}
					current = tt.after
					return nil, nil
				},
			}
			line, err := r.refresh("/usr/local/bin/senhub-agent")
			if ran != tt.wantRun {
				t.Fatalf("refresh-unit ran = %v, want %v", ran, tt.wantRun)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("err = %v, want refresh-unit's own output", err)
			}
			if (line != "") != tt.wantLine {
				t.Fatalf("line = %q, want a line %v", line, tt.wantLine)
			}
		})
	}
}

// A host carries drop-ins next to the CLI unit (credentials, socket
// visibility). Refreshing the unit, by refresh-unit or by update, rewrites
// the unit file only; the drop-ins stay byte for byte, and the reload is
// what makes systemd apply them to the new unit.
func TestWriteRefreshedUnitKeepsDropIns(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "senhub-agent.service")
	dropDir := unit + ".d"
	if err := os.MkdirAll(dropDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dropIns := map[string]string{
		"bearer.conf":            "[Service]\nEnvironmentFile=/etc/senhub-agent/bearer.env\n",
		"socket-visibility.conf": "[Service]\nPrivateNetwork=no\nRestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK\n",
	}
	for name, content := range dropIns {
		if err := os.WriteFile(filepath.Join(dropDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, err := os.ReadFile(filepath.Join("..", "internal", "agent", "probes", "cpu", "testdata", "senhub-agent-0.6.1.service"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, old, 0o644); err != nil {
		t.Fatal(err)
	}

	refreshed := refreshedUnit(string(old), func(string) bool { return true })
	reloaded := false
	if err := writeRefreshedUnit(unit, refreshed, func() error { reloaded = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !reloaded {
		t.Fatal("systemd was not reloaded")
	}
	got, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != refreshed || string(got) == string(old) {
		t.Fatal("the unit file was not replaced by the refreshed unit")
	}
	if !strings.Contains(string(got), "SystemCallErrorNumber=EPERM") || strings.Contains(string(got), "ProtectClock=true") {
		t.Fatal("the refreshed unit still kills the agent on adjtimex")
	}
	entries, err := os.ReadDir(dropDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(dropIns) {
		t.Fatalf("drop-in directory holds %d files, want %d", len(entries), len(dropIns))
	}
	for name, want := range dropIns {
		raw, err := os.ReadFile(filepath.Join(dropDir, name))
		if err != nil {
			t.Fatalf("drop-in %s: %v", name, err)
		}
		if string(raw) != want {
			t.Errorf("drop-in %s changed", name)
		}
	}
}

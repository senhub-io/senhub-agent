//go:build linux

package cpu

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUnitKillsOnAdjtimex(t *testing.T) {
	old := readFile(t, filepath.Join("testdata", "senhub-agent-0.6.1.service"))
	current := readFile(t, filepath.Join("..", "..", "..", "..", "app", "senhub-agent.service"))
	tests := []struct {
		name string
		unit string
		want bool
	}{
		{"0.6.1 unit: ProtectClock and an allow-list without adjtimex", old, true},
		{"current unit", current, false},
		{"0.6.1 unit with an errno drop-in", old + "\n[Service]\nSystemCallErrorNumber=EPERM\n", false},
		{"0.6.1 unit with ProtectClock turned off only", old + "\n[Service]\nProtectClock=false\n", true},
		{"0.6.1 unit, ProtectClock off and adjtimex allowed", old + "\n[Service]\nProtectClock=no\nSystemCallFilter=adjtimex\n", false},
		{"filter reset by an empty assignment", "[Service]\nSystemCallFilter=@system-service\nSystemCallFilter=\n", false},
		{"deny list naming @clock", "[Service]\nSystemCallFilter=~@clock @mount\n", true},
		{"deny list without the clock", "[Service]\nSystemCallFilter=~@mount\n", false},
		{"no filter at all", "[Service]\nExecStart=/usr/local/bin/senhub-agent run\n", false},
		{"directive outside [Service] ignored", "[Unit]\nProtectClock=true\n[Service]\nUser=senhub\n", false},
		{"errno kill keeps killing", "[Service]\nProtectClock=yes\nSystemCallErrorNumber=kill\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unitKillsOnAdjtimex(tt.unit); got != tt.want {
				t.Fatalf("unitKillsOnAdjtimex = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadUnitWithDropIns(t *testing.T) {
	etc, vendor := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(vendor, agentUnitName), "[Service]\nProtectClock=true\n")
	write(filepath.Join(vendor, agentUnitName+".d", "10-errno.conf"), "[Service]\nSystemCallErrorNumber=EPERM\n")
	write(filepath.Join(etc, agentUnitName+".d", "10-errno.conf"), "[Service]\n# shadows the vendor drop-in\n")

	text, ok := readUnitWithDropIns(agentUnitName, []string{etc, vendor})
	if !ok {
		t.Fatal("unit not found")
	}
	if !unitKillsOnAdjtimex(text) {
		t.Fatal("the /etc drop-in should shadow the vendor one that set an errno")
	}
	if _, ok := readUnitWithDropIns("absent.service", []string{etc, vendor}); ok {
		t.Fatal("a missing unit should not be found")
	}
}

func TestRunsUnderUnit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cgroup")
	if err := os.WriteFile(path, []byte("0::/system.slice/senhub-agent.service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !runsUnderUnit(path, agentUnitName) {
		t.Fatal("the agent's cgroup should match its unit")
	}
	if err := os.WriteFile(path, []byte("0::/user.slice/user-1000.slice/session-3.scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runsUnderUnit(path, agentUnitName) {
		t.Fatal("a login session is not the agent's unit")
	}
}

func TestAdjtimexSurvivableBelievesTheUnit(t *testing.T) {
	called := false
	err := adjtimexSurvivable(writeStatus(t, "2"), func() bool { return true }, func() error {
		called = true
		return nil
	})
	if called {
		t.Fatal("a unit known to kill on adjtimex must not cost a child")
	}
	if !errors.Is(err, errAdjtimexForbidden) {
		t.Fatalf("err = %v, want %v", err, errAdjtimexForbidden)
	}
}

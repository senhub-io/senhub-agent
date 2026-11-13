//go:build linux

package cpu

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeStatus(t *testing.T, seccomp string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "status")
	content := "Name:\tsenhub-agent\nNoNewPrivs:\t1\n"
	if seccomp != "" {
		content += "Seccomp:\t" + seccomp + "\nSeccomp_filters:\t1\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSeccompMode(t *testing.T) {
	if got := seccompMode(writeStatus(t, "2")); got != "2" {
		t.Fatalf("seccompMode = %q, want 2", got)
	}
	if got := seccompMode(writeStatus(t, "")); got != "" {
		t.Fatalf("seccompMode without the field = %q, want empty", got)
	}
	if got := seccompMode(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("seccompMode of a missing file = %q, want empty", got)
	}
}

func TestAdjtimexSurvivable(t *testing.T) {
	shell := func(script string) func() error {
		return func() error { return exec.Command("sh", "-c", script).Run() }
	}
	tests := []struct {
		name      string
		seccomp   string
		child     func() error
		wantChild bool
		wantErr   error
		wantAnyEr bool
	}{
		{name: "no filter skips the child", seccomp: "0", child: shell("exit 0")},
		{name: "filter, child succeeds", seccomp: "2", child: shell("exit 0"), wantChild: true},
		{name: "filter, child gets EPERM", seccomp: "2", child: shell("exit 1"), wantChild: true},
		{name: "filter, child killed by SIGSYS", seccomp: "2", child: shell("kill -SYS $$"), wantChild: true, wantErr: errAdjtimexForbidden},
		{name: "unreadable status still probes", seccomp: "", child: shell("kill -SYS $$"), wantChild: true, wantErr: errAdjtimexForbidden},
		{name: "child cannot start", seccomp: "2", child: func() error { return exec.Command("/nonexistent/senhub").Run() }, wantChild: true, wantAnyEr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			err := adjtimexSurvivable(writeStatus(t, tt.seccomp), func() bool { return false }, func() error {
				called = true
				return tt.child()
			})
			if called != tt.wantChild {
				t.Fatalf("child called = %v, want %v", called, tt.wantChild)
			}
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAnyEr:
				if err == nil || errors.Is(err, errAdjtimexForbidden) {
					t.Fatalf("err = %v, want a start error", err)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			}
		})
	}
}

func TestLinuxClockSyncChecksOnceAndKeepsTheVerdict(t *testing.T) {
	calls := 0
	s := &linuxClockSync{
		root: t.TempDir(),
		survivable: func() error {
			calls++
			return errAdjtimexForbidden
		},
	}
	for range 3 {
		if _, err := s.read(); !errors.Is(err, errAdjtimexForbidden) {
			t.Fatalf("read err = %v, want errAdjtimexForbidden", err)
		}
	}
	if calls != 1 {
		t.Fatalf("survivable called %d times, want 1", calls)
	}
}

// TestMain mirrors app.Main: the guard re-executes the test binary as its
// child, which must make the call and exit instead of running the tests.
func TestMain(m *testing.M) {
	RunAdjtimexChild()
	os.Exit(m.Run())
}

// TestClockSyncUnderKillingFilter runs where adjtimex kills the process, as
// under a 0.6.3 unit. It needs that filter applied from outside, so it only
// runs when SENHUB_TEST_ADJTIMEX_KILLED is set, e.g. in docker with
// --security-opt seccomp= a profile whose adjtimex action is
// SCMP_ACT_KILL_PROCESS.
func TestClockSyncUnderKillingFilter(t *testing.T) {
	if os.Getenv("SENHUB_TEST_ADJTIMEX_KILLED") != "1" {
		t.Skip("needs a seccomp filter that kills on adjtimex")
	}
	if _, err := newClockSync()(); !errors.Is(err, errAdjtimexForbidden) {
		t.Fatalf("read err = %v, want errAdjtimexForbidden", err)
	}
}

package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func pathDeps(versions map[string]string, onPath ...string) doctorDeps {
	d := healthyDoctorDeps()
	d.install.exeLink = func(string) (string, error) { return "/var/lib/senhub-agent/bin/senhub-agent", nil }
	d.install.binVersion = func(p string) string { return versions[p] }
	d.install.pathBinaries = func() []string { return onPath }
	return d
}

// An old copy left in /usr/local/bin answers `senhub-agent --version` for a
// service that runs something else (#607): the warning names both paths and
// both versions and says how to fix it.
func TestDoctorPathBinary_StaleCopyIsAWarning(t *testing.T) {
	d := pathDeps(map[string]string{
		"/var/lib/senhub-agent/bin/senhub-agent": "0.5.3",
		"/usr/local/bin/senhub-agent":            "0.4.1",
	}, "/usr/local/bin/senhub-agent")

	c := expectLevel(t, runDoctorChecks(d), "install.path", levelWarn)
	for _, want := range []string{"/var/lib/senhub-agent/bin/senhub-agent", "0.5.3", "/usr/local/bin/senhub-agent", "0.4.1"} {
		if !strings.Contains(c.Message, want) {
			t.Errorf("message %q lacks %q", c.Message, want)
		}
	}
	if !strings.Contains(c.Fix, "/var/lib/senhub-agent/bin/senhub-agent") {
		t.Errorf("fix %q should point to the service binary", c.Fix)
	}
}

func TestDoctorPathBinary_SameVersionIsFine(t *testing.T) {
	d := pathDeps(map[string]string{
		"/var/lib/senhub-agent/bin/senhub-agent": "0.6.3",
		"/usr/local/bin/senhub-agent":            "0.6.3",
	}, "/usr/local/bin/senhub-agent")
	expectLevel(t, runDoctorChecks(d), "install.path", levelOK)
}

func TestDoctorPathBinary_TheServiceBinaryItselfIsNotStale(t *testing.T) {
	d := pathDeps(map[string]string{"/var/lib/senhub-agent/bin/senhub-agent": "0.6.3"},
		"/var/lib/senhub-agent/bin/senhub-agent")
	expectLevel(t, runDoctorChecks(d), "install.path", levelOK)
}

// With the service stopped the unit's ExecStart names the reference.
func TestDoctorPathBinary_FallsBackToTheUnitExecStart(t *testing.T) {
	exec := unitExecBinary(packagedSystemdUnit)
	if exec == "" {
		t.Fatal("the packaged unit has no ExecStart binary")
	}
	d := pathDeps(map[string]string{exec: "0.6.3", "/opt/old/senhub-agent": "0.4.1"}, "/opt/old/senhub-agent")
	d.status = func() statusResult { return statusResult{notRunning: true, serviceState: "stopped"} }
	c := expectLevel(t, runDoctorChecks(d), "install.path", levelWarn)
	if !strings.Contains(c.Message, exec) || !strings.Contains(c.Message, "/opt/old/senhub-agent") {
		t.Errorf("message %q must name both paths", c.Message)
	}
}

func TestBinariesOnPath_FindsEachFileOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	a, b := t.TempDir(), t.TempDir()
	real := filepath.Join(a, "senhub-agent")
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(b, "senhub-agent")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{a, b, t.TempDir()}, string(os.PathListSeparator)))
	got := binariesOnPath()
	if len(got) != 1 || got[0] != real {
		t.Errorf("binariesOnPath = %v, want just %s", got, real)
	}
}

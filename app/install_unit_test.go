package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kardianos/service"
)

var testServiceArgs = []string{"run", "--config-path", "/etc/senhub-agent/agent.yaml"}

// fakeUnitHost is an in-memory service manager: one unit file, an enabled
// flag, and a count of every mutation.
type fakeUnitHost struct {
	unit        string
	enabled     bool
	packageOwn  bool
	writes      int
	reloads     int
	enableCalls int
	writeErr    error
}

func (h *fakeUnitHost) ops() unitOps {
	return unitOps{
		unitPath:     "/etc/systemd/system/senhub-agent.service",
		packageOwned: h.packageOwn,
		read:         func() ([]byte, error) { return []byte(h.unit), nil },
		write: func(data []byte) error {
			if h.writeErr != nil {
				return h.writeErr
			}
			h.writes++
			h.unit = string(data)
			return nil
		},
		reload:  func() error { h.reloads++; return nil },
		enabled: func() bool { return h.enabled },
		enable:  func() error { h.enableCalls++; h.enabled = true; return nil },
	}
}

func desiredFor(t *testing.T, user string) string {
	t.Helper()
	unit, err := desiredInstallUnit(user, testServiceArgs)
	if err != nil {
		t.Fatalf("rendering the unit for %s: %v", user, err)
	}
	return unit
}

func TestRenderedUnitCarriesResolvedPaths(t *testing.T) {
	for _, user := range []string{defaultServiceUser, rootServiceUser} {
		unit := desiredFor(t, user)
		wantExec := "ExecStart=/usr/local/bin/senhub-agent \"run\" \"--config-path\" \"/etc/senhub-agent/agent.yaml\""
		if !strings.Contains(unit, wantExec) {
			t.Errorf("%s unit lacks %q:\n%s", user, wantExec, unit)
		}
		if !strings.Contains(unit, "WorkingDirectory=/usr/local/bin") {
			t.Errorf("%s unit lacks the working directory", user)
		}
	}
}

func TestReconcileLeavesACurrentServiceAlone(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	host := &fakeUnitHost{unit: desired, enabled: true}
	changed, err := reconcileInstalledService(host.ops(), desired)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(changed) != 0 || host.writes+host.reloads+host.enableCalls != 0 {
		t.Errorf("a current service was touched: changed=%v host=%+v", changed, host)
	}
}

func TestReconcileKeepsOperatorDirectives(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	extended := strings.Replace(desired, "[Install]", "EnvironmentFile=/etc/senhub-agent/env\n\n[Install]", 1)
	if !unitMatches(extended, desired) {
		t.Fatal("an operator-added directive was taken for drift")
	}
}

func TestReconcileRewritesADriftedUnitOnce(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	drifted := strings.Replace(desired, "\nRestartSec=5\n", "\nRestartSec=99\n", 1)
	if drifted == desired {
		t.Fatal("test unit has no RestartSec= line to drift")
	}
	host := &fakeUnitHost{unit: drifted, enabled: true}

	changed, err := reconcileInstalledService(host.ops(), desired)
	if err != nil || len(changed) != 1 {
		t.Fatalf("first run: changed=%v err=%v; want the unit rewritten", changed, err)
	}
	if host.writes != 1 || host.reloads != 1 || host.enableCalls != 0 {
		t.Errorf("first run side effects: %+v", host)
	}

	changed, err = reconcileInstalledService(host.ops(), desired)
	if err != nil || len(changed) != 0 || host.writes != 1 {
		t.Errorf("second run: changed=%v err=%v writes=%d; want nothing", changed, err, host.writes)
	}
}

func TestReconcileDriftKeepsOperatorDirectivesAndConverges(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	drifted := strings.Replace(desired, "\nRestartSec=5\n", "\nRestartSec=99\nEnvironmentFile=/etc/senhub-agent/env\n", 1)
	host := &fakeUnitHost{unit: drifted, enabled: true}
	if _, err := reconcileInstalledService(host.ops(), desired); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !strings.Contains(host.unit, "EnvironmentFile=/etc/senhub-agent/env") {
		t.Error("the operator's EnvironmentFile= was dropped by the rewrite")
	}
	if !strings.Contains(host.unit, "RestartSec=5") {
		t.Error("the managed directive was not restored")
	}
	if changed, _ := reconcileInstalledService(host.ops(), desired); len(changed) != 0 {
		t.Errorf("the rewrite does not converge: %v", changed)
	}
}

func TestReconcileEnablesADisabledService(t *testing.T) {
	desired := desiredFor(t, rootServiceUser)
	host := &fakeUnitHost{unit: desired, enabled: false}

	changed, err := reconcileInstalledService(host.ops(), desired)
	if err != nil || len(changed) != 1 || host.enableCalls != 1 || host.writes != 0 {
		t.Fatalf("first run: changed=%v err=%v host=%+v; want only an enable", changed, err, host)
	}
	if changed, _ = reconcileInstalledService(host.ops(), desired); len(changed) != 0 || host.enableCalls != 1 {
		t.Errorf("second run enabled again: %v", changed)
	}
}

func TestReconcileNeverRewritesAPackageUnit(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	host := &fakeUnitHost{unit: "[Service]\nExecStart=/usr/bin/other\n", enabled: true, packageOwn: true}
	changed, err := reconcileInstalledService(host.ops(), desired)
	if err != nil || len(changed) != 0 || host.writes != 0 {
		t.Errorf("a package-owned unit was rewritten: changed=%v err=%v", changed, err)
	}
}

func TestReconcilePropagatesAWriteFailure(t *testing.T) {
	desired := desiredFor(t, defaultServiceUser)
	host := &fakeUnitHost{unit: "[Service]\n", enabled: true, writeErr: errors.New("read-only file system")}
	if _, err := reconcileInstalledService(host.ops(), desired); err == nil {
		t.Error("a failed unit write was reported as success")
	}
}

func TestInstallStateTreatsDriftAndDisabledAsNotDone(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(cfg, []byte(testAgentYAML), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	yes := func() bool { return true }
	no := func() bool { return false }
	running := installFakeService{state: service.StatusRunning}

	cases := []struct {
		name   string
		probes installProbes
		want   bool
	}{
		{"all current", installProbes{yes, yes, yes}, true},
		{"unit drifted", installProbes{yes, no, yes}, false},
		{"service disabled", installProbes{yes, yes, no}, false},
		{"binary differs", installProbes{no, yes, yes}, false},
		{"no probes at all", installProbes{}, true},
	}
	for _, c := range cases {
		if got := detectInstallState(running, cfg, c.probes).alreadyDone(); got != c.want {
			t.Errorf("%s: alreadyDone = %v, want %v", c.name, got, c.want)
		}
	}
}

// A Windows registration has no unit file and one binary: the install
// state is decided by the service manager's answer and the configuration
// alone. The service fake stands in for the SCM and the path is a
// Windows-shaped one, whatever OS runs the test.
func TestInstallStateWindowsRegistration(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "SenHub", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o750); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.WriteFile(cfg, []byte(testAgentYAML), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	yes := func() bool { return true }
	windowsProbes := installProbes{binaryCurrent: yes, unitCurrent: yes, serviceEnabled: yes}

	cases := []struct {
		name string
		svc  installFakeService
		want bool
	}{
		{"registered and stopped", installFakeService{state: service.StatusStopped}, true},
		{"registered and running", installFakeService{state: service.StatusRunning}, true},
		{"not registered", installFakeService{err: service.ErrNotInstalled}, false},
	}
	for _, c := range cases {
		if got := detectInstallState(c.svc, cfg, windowsProbes).alreadyDone(); got != c.want {
			t.Errorf("%s: alreadyDone = %v, want %v", c.name, got, c.want)
		}
	}
}

package osupdates

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestModuleLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}), "probe.os_updates.test")
}

// fakeExitError mimics *exec.ExitError for the exitCode helper: any
// error exposing ExitCode() int satisfies it.
type fakeExitError struct{ code int }

func (e fakeExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e fakeExitError) ExitCode() int { return e.code }

// fakeEnv builds a linuxCollector whose seams are driven by maps:
// commands available in PATH, files that exist, and per-command runner
// outputs keyed by "name arg1 arg2".
type fakeEnv struct {
	commands map[string]bool
	files    map[string]bool
	outputs  map[string]string
	errs     map[string]error
	calls    []string
}

func (f *fakeEnv) collector() *linuxCollector {
	return &linuxCollector{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := strings.TrimSpace(name + " " + strings.Join(args, " "))
			f.calls = append(f.calls, key)
			if err, ok := f.errs[key]; ok {
				return []byte(f.outputs[key]), err
			}
			out, ok := f.outputs[key]
			if !ok {
				return nil, fmt.Errorf("fakeEnv: unexpected command %q", key)
			}
			return []byte(out), nil
		},
		lookPath: func(name string) (string, error) {
			if f.commands[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found in PATH")
		},
		fileExists: func(path string) bool { return f.files[path] },
		logger:     newTestModuleLogger(),
	}
}

func TestParseAptCheck(t *testing.T) {
	cases := []struct {
		name          string
		out           string
		wantPending   int
		wantSecurity  int
		wantParseFail bool
	}{
		{name: "typical", out: "42;7", wantPending: 42, wantSecurity: 7},
		{name: "zero", out: "0;0\n", wantPending: 0, wantSecurity: 0},
		{name: "trailing newline and spaces", out: " 12;3 \n", wantPending: 12, wantSecurity: 3},
		{name: "garbage", out: "E: something broke", wantParseFail: true},
		{name: "missing field", out: "42", wantParseFail: true},
		{name: "non numeric", out: "a;b", wantParseFail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending, security, err := parseAptCheck(tc.out)
			if tc.wantParseFail {
				if err == nil {
					t.Fatalf("expected parse error, got pending=%d security=%d", pending, security)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAptCheck(%q): %v", tc.out, err)
			}
			if pending != tc.wantPending || security != tc.wantSecurity {
				t.Errorf("parseAptCheck(%q) = (%d, %d), want (%d, %d)",
					tc.out, pending, security, tc.wantPending, tc.wantSecurity)
			}
		})
	}
}

// Golden output shape: Debian 12 `apt-get -s upgrade` with two packages
// coming from the security archive.
const aptSimulationGolden = `Reading package lists...
Building dependency tree...
Reading state information...
Calculating upgrade...
The following packages will be upgraded:
  base-files libssl3 openssl tzdata
4 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Inst base-files [12.4+deb12u4] (12.4+deb12u5 Debian:12.5/stable [amd64])
Inst libssl3 [3.0.11-1~deb12u2] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Inst openssl [3.0.11-1~deb12u2] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Inst tzdata [2023c-5+deb12u1] (2024a-0+deb12u1 Debian:12.5/stable-updates [amd64])
Conf base-files (12.4+deb12u5 Debian:12.5/stable [amd64])
Conf libssl3 (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Conf openssl (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
Conf tzdata (2024a-0+deb12u1 Debian:12.5/stable-updates [amd64])
`

func TestParseAptSimulation_CountsInstAndSecurityLines(t *testing.T) {
	pending, security := parseAptSimulation(aptSimulationGolden)
	if pending != 4 {
		t.Errorf("pending: got %d, want 4", pending)
	}
	if security != 2 {
		t.Errorf("security: got %d, want 2", security)
	}
}

func TestParseAptSimulation_NoUpdates(t *testing.T) {
	out := `Reading package lists...
Building dependency tree...
Reading state information...
Calculating upgrade...
0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
`
	pending, security := parseAptSimulation(out)
	if pending != 0 || security != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", pending, security)
	}
}

func TestLinuxCollector_AptViaAptCheck(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"apt-get": true},
		files: map[string]bool{
			aptCheckPath:       true,
			rebootRequiredFile: true,
		},
		outputs: map[string]string{aptCheckPath: "42;7"},
	}
	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := updatesStatus{pending: 42, pendingSecurity: 7, rebootRequired: true, packageManager: "apt"}
	if status != want {
		t.Errorf("status = %+v, want %+v", status, want)
	}
}

func TestLinuxCollector_AptFallbackToSimulation(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"apt-get": true},
		files:    map[string]bool{}, // no apt-check, no reboot-required
		outputs:  map[string]string{"apt-get -s upgrade": aptSimulationGolden},
	}
	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := updatesStatus{pending: 4, pendingSecurity: 2, rebootRequired: false, packageManager: "apt"}
	if status != want {
		t.Errorf("status = %+v, want %+v", status, want)
	}
}

func TestLinuxCollector_AptCheckFailureFallsBackToSimulation(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"apt-get": true},
		files:    map[string]bool{aptCheckPath: true},
		outputs:  map[string]string{"apt-get -s upgrade": aptSimulationGolden},
		errs:     map[string]error{aptCheckPath: fakeExitError{code: 2}},
	}
	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if status.pending != 4 || status.pendingSecurity != 2 {
		t.Errorf("status = %+v, want pending=4 security=2 from the simulation fallback", status)
	}
}

// Golden output shape: RHEL 9 `dnf -q updateinfo list` including the
// subscription-manager banner and metadata timestamp noise lines.
const dnfUpdateinfoGolden = `Updating Subscription Management repositories.
Last metadata expiration check: 0:12:34 ago on Tue 21 Jul 2026 10:00:00 CEST.
RHSA-2026:0123 Important/Sec. openssl-libs-1:3.0.7-27.el9_3.x86_64
RHSA-2026:0456 Moderate/Sec.  curl-7.76.1-26.el9_3.x86_64
RHBA-2026:0789 bugfix         tzdata-2026a-1.el9.noarch
RHEA-2026:0999 enhancement    systemd-252-32.el9_3.x86_64
`

const dnfUpdateinfoSecurityGolden = `Updating Subscription Management repositories.
Last metadata expiration check: 0:12:34 ago on Tue 21 Jul 2026 10:00:00 CEST.
RHSA-2026:0123 Important/Sec. openssl-libs-1:3.0.7-27.el9_3.x86_64
RHSA-2026:0456 Moderate/Sec.  curl-7.76.1-26.el9_3.x86_64
`

func TestCountAdvisoryLines(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want int
	}{
		{name: "rhel9 with noise", out: dnfUpdateinfoGolden, want: 4},
		{name: "security only", out: dnfUpdateinfoSecurityGolden, want: 2},
		{name: "empty", out: "", want: 0},
		{name: "noise only", out: "Last metadata expiration check: 0:01:00 ago on Tue.\n", want: 0},
		{name: "dnf5 column header skipped", out: "Name Type Severity Package\nFEDORA-2026-1a2b3c4d Security Important openssl-1:3.0.9-2.fc40.x86_64\n", want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countAdvisoryLines(tc.out); got != tc.want {
				t.Errorf("countAdvisoryLines = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLinuxCollector_DnfWithRebootRequired(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"dnf": true, "needs-restarting": true},
		files:    map[string]bool{},
		outputs: map[string]string{
			"dnf -q updateinfo list":            dnfUpdateinfoGolden,
			"dnf -q updateinfo list --security": dnfUpdateinfoSecurityGolden,
		},
		errs: map[string]error{"needs-restarting -r": fakeExitError{code: 1}},
	}
	// needs-restarting -r has both an entry in errs and none in outputs;
	// give it an empty output so the fake runner returns the error path.
	env.outputs["needs-restarting -r"] = ""

	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := updatesStatus{pending: 4, pendingSecurity: 2, rebootRequired: true, packageManager: "dnf"}
	if status != want {
		t.Errorf("status = %+v, want %+v", status, want)
	}
}

func TestLinuxCollector_DnfNoRebootNeeded(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"dnf": true, "needs-restarting": true},
		files:    map[string]bool{},
		outputs: map[string]string{
			"dnf -q updateinfo list":            "",
			"dnf -q updateinfo list --security": "",
			"needs-restarting -r":               "No core libraries or services have been updated.",
		},
	}
	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := updatesStatus{pending: 0, pendingSecurity: 0, rebootRequired: false, packageManager: "dnf"}
	if status != want {
		t.Errorf("status = %+v, want %+v", status, want)
	}
}

func TestLinuxCollector_YumUsesSameSurface(t *testing.T) {
	env := &fakeEnv{
		commands: map[string]bool{"yum": true},
		files:    map[string]bool{},
		outputs: map[string]string{
			"yum -q updateinfo list":            dnfUpdateinfoGolden,
			"yum -q updateinfo list --security": dnfUpdateinfoSecurityGolden,
		},
	}
	status, err := env.collector().collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if status.packageManager != "yum" || status.pending != 4 || status.pendingSecurity != 2 {
		t.Errorf("status = %+v, want yum/4/2", status)
	}
	if status.rebootRequired {
		t.Error("rebootRequired should be false when needs-restarting is not installed")
	}
}

func TestLinuxCollector_NoPackageManagerFound(t *testing.T) {
	env := &fakeEnv{commands: map[string]bool{}, files: map[string]bool{}}
	_, err := env.collector().collect(context.Background())
	if err == nil {
		t.Fatal("expected an error when no package manager is found")
	}
}

func TestExitCode(t *testing.T) {
	if got := exitCode(fakeExitError{code: 1}); got != 1 {
		t.Errorf("exitCode(fakeExitError{1}) = %d, want 1", got)
	}
	if got := exitCode(errors.New("plain")); got != -1 {
		t.Errorf("exitCode(plain error) = %d, want -1", got)
	}
	if got := exitCode(fmt.Errorf("wrapped: %w", fakeExitError{code: 100})); got != 100 {
		t.Errorf("exitCode(wrapped) = %d, want 100", got)
	}
}

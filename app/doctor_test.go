package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/status"
	"senhub-agent.go/internal/cliexit"
)

var doctorNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func runningStatus() statusResult {
	return statusResult{
		serviceState: "running",
		source:       "daemon",
		system: status.SystemStatus{
			Probes: []status.ProbeStatus{
				{Name: "cpu", Status: "active", MetricsCount: 12, LastUpdate: doctorNow.Add(-30 * time.Second)},
			},
			Performance: status.PerformanceInfo{Measured: true, MemoryUsageMB: 41, HeapMB: 12},
		},
	}
}

// healthyDoctorDeps is a host on which every check passes.
func healthyDoctorDeps() doctorDeps {
	return doctorDeps{
		configPath: "/etc/senhub-agent/agent.yaml",
		now:        func() time.Time { return doctorNow },
		cliVersion: "0.6.1",
		logDir:     "/var/log/senhub-agent",
		configCheck: func(string) (string, checkOutcome, error) {
			return "  [OK]   config_version: 3\n  [INFO] agent.license not set (free tier)\n", checkOutcome{}, nil
		},
		loadConfig: func(string) (configuration.LocalConfigurationData, error) {
			return configuration.LocalConfigurationData{
				Storage: []configuration.StorageConfig{
					{Name: "http", Params: map[string]interface{}{"port": 8080}},
					{Name: "otlp", Params: map[string]interface{}{"endpoint": "collector:4317"}},
				},
				Probes: []configuration.ProbeConfig{{Name: "cpu", Type: "cpu"}},
			}, nil
		},
		status:      runningStatus,
		httpListen:  func(string) (string, string, int) { return "http", "127.0.0.1", 8080 },
		portFree:    func(string, int) error { return nil },
		secretStore: func(string) (secretReport, error) { return secretReport{}, nil },
		licenseExpiry: func(string) (string, time.Time, error) {
			return "pro", doctorNow.Add(200 * 24 * time.Hour), nil
		},
		outputTest: func(context.Context, string, map[string]interface{}, string, int) []otlp.ConnectionStep {
			return []otlp.ConnectionStep{{Name: "dns", Passed: true}, {Name: "tcp", Passed: true}}
		},
		freePercent: func(string) (float64, error) { return 55, nil },
		install: installProbe{
			unit:         func() (string, error) { return packagedSystemdUnit, nil },
			binaryExists: func(string) bool { return true },
			mainPID:      func() (string, error) { return "4242", nil },
			exeLink:      func(string) (string, error) { return "/usr/local/bin/senhub-agent", nil },
			sameContents: func(a, b string) (bool, error) { return true, nil },
			logGroup:     "adm",
			groupMember:  func(string) (bool, bool, error) { return true, true, nil },
			logDirAccess: func(string, string) (logDirState, error) {
				return logDirState{Exists: true, Writable: true, Detail: "owned by senhub"}, nil
			},
			serviceBin: func() string { return "" },
			binVersion: func(string) string { return "" },
		},
	}
}

func findCheck(t *testing.T, checks []doctorCheck, id string) doctorCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, checks)
	return doctorCheck{}
}

func expectLevel(t *testing.T, checks []doctorCheck, id, level string) doctorCheck {
	t.Helper()
	c := findCheck(t, checks, id)
	if c.Level != level {
		t.Errorf("%s: level %q (%s), want %q", id, c.Level, c.Message, level)
	}
	if level != levelOK && level != levelSkip && c.Fix == "" {
		t.Errorf("%s: a %s check must say how to fix it", id, level)
	}
	return c
}

func TestDoctorHealthyHostExitsOK(t *testing.T) {
	checks := runDoctorChecks(healthyDoctorDeps())
	for _, c := range checks {
		if c.Level != levelOK && c.ID != "outputs.http" {
			t.Errorf("%s is %s on a healthy host: %s", c.ID, c.Level, c.Message)
		}
	}
	if code := doctorExitCode(checks); code != cliexit.OK {
		t.Errorf("exit code %d, want %d", code, cliexit.OK)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		levels []string
		want   int
	}{
		{"all ok", []string{levelOK, levelOK}, cliexit.OK},
		{"skip does not count", []string{levelOK, levelSkip}, cliexit.OK},
		{"warning", []string{levelOK, levelWarn, levelSkip}, cliexit.Warning},
		{"failure", []string{levelOK, levelFail}, cliexit.Failure},
		{"failure outranks warning", []string{levelWarn, levelFail}, cliexit.Failure},
		{"nothing", nil, cliexit.OK},
	}
	for _, tt := range tests {
		var checks []doctorCheck
		for _, l := range tt.levels {
			checks = append(checks, doctorCheck{Level: l})
		}
		if got := doctorExitCode(checks); got != tt.want {
			t.Errorf("%s: exit code %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestDoctorJSONShape(t *testing.T) {
	d := healthyDoctorDeps()
	d.portFree = func(string, int) error { return errors.New("port 8080 is not available") }
	d.status = func() statusResult { return statusResult{serviceState: "stopped", notRunning: true} }

	var out bytes.Buffer
	code := executeDoctor(d, true, &out)

	var doc struct {
		Schema   string        `json:"schema"`
		OK       bool          `json:"ok"`
		Status   string        `json:"status"`
		ExitCode int           `json:"exit_code"`
		Checks   []doctorCheck `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out.String())
	}
	if doc.Schema != "senhub.cli.doctor/v1" {
		t.Errorf("schema %q", doc.Schema)
	}
	if doc.ExitCode != code || code != cliexit.Failure || doc.Status != "failure" || doc.OK {
		t.Errorf("header %+v with code %d", doc, code)
	}
	if len(doc.Checks) == 0 {
		t.Fatal("no checks")
	}
	var raw struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"section", "id", "level", "message", "fix"} {
		if _, ok := raw.Checks[0][key]; !ok {
			t.Errorf("a check has no %q field", key)
		}
	}
}

func TestDoctorTextOutputGroupsBySection(t *testing.T) {
	var out bytes.Buffer
	executeDoctor(healthyDoctorDeps(), false, &out)
	text := out.String()
	last := -1
	for _, title := range []string{"Install", "Configuration", "Outputs", "Probes", "Host"} {
		i := strings.Index(text, "\n"+title+"\n")
		if i < 0 || i < last {
			t.Fatalf("section %q missing or out of order:\n%s", title, text)
		}
		last = i
	}
	if !strings.Contains(text, "  [OK]   install.service: ") {
		t.Errorf("a line does not follow the config check layout:\n%s", text)
	}
	if strings.Contains(text, "\"") && strings.Contains(text, "{") {
		t.Errorf("text mode printed JSON:\n%s", text)
	}
}

func TestDoctorServiceStopped(t *testing.T) {
	d := healthyDoctorDeps()
	d.status = func() statusResult { return statusResult{serviceState: "stopped", notRunning: true} }
	checks := runDoctorChecks(d)

	expectLevel(t, checks, "install.service", levelWarn)
	expectLevel(t, checks, "install.binary", levelSkip)
	c := expectLevel(t, checks, "probes.status", levelSkip)
	if !strings.Contains(c.Message, "stopped") {
		t.Errorf("the skip does not say why: %s", c.Message)
	}
	expectLevel(t, checks, "host.memory", levelSkip)
	expectLevel(t, checks, "config.http_port", levelOK)
	expectLevel(t, checks, "outputs.otlp", levelOK)
	if code := doctorExitCode(checks); code != cliexit.Warning {
		t.Errorf("exit code %d, want %d", code, cliexit.Warning)
	}
}

func TestDoctorServiceNotInstalled(t *testing.T) {
	d := healthyDoctorDeps()
	d.status = func() statusResult { return statusResult{managerErr: service.ErrNotInstalled} }
	checks := runDoctorChecks(d)
	c := expectLevel(t, checks, "install.service", levelWarn)
	if c.Fix != "senhub-agent install" {
		t.Errorf("fix %q", c.Fix)
	}
	expectLevel(t, checks, "probes.status", levelSkip)
}

func TestDoctorNoServiceManager(t *testing.T) {
	d := healthyDoctorDeps()
	d.status = func() statusResult { return statusResult{managerErr: errors.New("no init system")} }
	checks := runDoctorChecks(d)
	expectLevel(t, checks, "install.service", levelSkip)
	expectLevel(t, checks, "probes.status", levelSkip)
}

func TestDoctorInstallSection(t *testing.T) {
	t.Run("unit drift", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.unit = func() (string, error) {
			return strings.Replace(packagedSystemdUnit, "NoNewPrivileges=true", "NoNewPrivileges=false", 1) + "\n", nil
		}
		c := expectLevel(t, runDoctorChecks(d), "install.unit", levelWarn)
		if c.Fix != "sudo senhub-agent refresh-unit" {
			t.Errorf("fix %q", c.Fix)
		}
	})
	t.Run("no unit installed", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.unit = func() (string, error) { return "", errors.New("no such file") }
		expectLevel(t, runDoctorChecks(d), "install.unit", levelSkip)
	})
	t.Run("binary replaced", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.exeLink = func(string) (string, error) { return "/usr/local/bin/senhub-agent (deleted)", nil }
		expectLevel(t, runDoctorChecks(d), "install.binary", levelFail)
	})
	t.Run("binary differs", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.sameContents = func(a, b string) (bool, error) { return false, nil }
		expectLevel(t, runDoctorChecks(d), "install.binary", levelFail)
	})
	t.Run("process unreadable without root", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.exeLink = func(string) (string, error) { return "", errors.New("permission denied") }
		c := expectLevel(t, runDoctorChecks(d), "install.binary", levelSkip)
		if !strings.Contains(c.Message, "root") {
			t.Errorf("the skip does not say what is needed: %s", c.Message)
		}
	})
	t.Run("version skew", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.serviceBin = func() string { return "/var/lib/senhub-agent/bin/senhub-agent" }
		d.install.binVersion = func(string) string { return "0.5.9" }
		expectLevel(t, runDoctorChecks(d), "install.skew", levelWarn)
		d.install.binVersion = func(string) string { return "0.6.1" }
		expectLevel(t, runDoctorChecks(d), "install.skew", levelOK)
	})
	t.Run("adm group", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.loadConfig = func(string) (configuration.LocalConfigurationData, error) {
			return configuration.LocalConfigurationData{Probes: []configuration.ProbeConfig{
				{Name: "syslog-files", Type: "filetail", Params: map[string]interface{}{"paths": []interface{}{"/var/log/syslog"}}},
			}}, nil
		}
		d.install.groupMember = func(string) (bool, bool, error) { return false, true, nil }
		checks := runDoctorChecks(d)
		c := expectLevel(t, checks, "install.log_group", levelWarn)
		if !strings.Contains(c.Fix, "usermod -aG adm senhub") {
			t.Errorf("fix %q", c.Fix)
		}
		d.install.groupMember = func(string) (bool, bool, error) { return true, true, nil }
		expectLevel(t, runDoctorChecks(d), "install.log_group", levelOK)
	})
	t.Run("adm group not needed without a system log probe", func(t *testing.T) {
		for _, c := range runDoctorChecks(healthyDoctorDeps()) {
			if c.ID == "install.log_group" {
				t.Errorf("reported with no probe on /var/log: %+v", c)
			}
		}
	})
	t.Run("log directory not writable", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.install.logDirAccess = func(string, string) (logDirState, error) {
			return logDirState{Exists: true, Detail: "owned by uid 0, mode 755"}, nil
		}
		expectLevel(t, runDoctorChecks(d), "install.log_dir", levelFail)
	})
}

func TestProbesReadingVarLog(t *testing.T) {
	probes := []configuration.ProbeConfig{
		{Name: "a", Type: "filetail", Params: map[string]interface{}{"path": "/var/log/auth.log"}},
		{Name: "b", Type: "filetail", Params: map[string]interface{}{"path": "/srv/app/app.log"}},
		{Name: "c", Type: "cpu", Params: map[string]interface{}{"path": "/var/log/syslog"}},
		{Name: "d", Type: "filetail", Params: map[string]interface{}{"path": "/var/log/senhub-agent/x.log"}},
	}
	got := probesReadingVarLog(probes)
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("got %v, want [a]", got)
	}
}

func TestDoctorConfigurationSection(t *testing.T) {
	t.Run("free tier is not a warning", func(t *testing.T) {
		c := expectLevel(t, runDoctorChecks(healthyDoctorDeps()), "config.license", levelOK)
		if !strings.Contains(c.Message, "free tier") {
			t.Errorf("message %q", c.Message)
		}
	})
	t.Run("errors and warnings", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.configCheck = func(string) (string, checkOutcome, error) {
			return "  [OK]   config_version: 3\n  [WARN] No probes configured\n  [ERROR] Storage \"otlp\": endpoint missing\n",
				checkOutcome{errors: 1, warnings: 1}, nil
		}
		checks := runDoctorChecks(d)
		var levels []string
		for _, c := range checks {
			if c.ID == "config.check" {
				levels = append(levels, c.Level)
			}
		}
		if len(levels) != 2 || levels[0] != levelWarn || levels[1] != levelFail {
			t.Errorf("config.check levels %v, want [warn fail]", levels)
		}
	})
	t.Run("unreadable file", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.configCheck = func(string) (string, checkOutcome, error) {
			return "", checkOutcome{loadErr: errors.New("cannot read file")}, nil
		}
		expectLevel(t, runDoctorChecks(d), "config.check", levelFail)
	})
	t.Run("secret store", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.secretStore = func(string) (secretReport, error) {
			return secretReport{Present: true, Backend: "age", Secrets: 3}, nil
		}
		c := expectLevel(t, runDoctorChecks(d), "config.secrets", levelOK)
		if !strings.Contains(c.Message, "age") {
			t.Errorf("message %q", c.Message)
		}
		d.secretStore = func(string) (secretReport, error) { return secretReport{}, errors.New("permission denied") }
		expectLevel(t, runDoctorChecks(d), "config.secrets", levelFail)
	})
	t.Run("licence expiry", func(t *testing.T) {
		d := healthyDoctorDeps()
		withLicense := func(string) (configuration.LocalConfigurationData, error) {
			return configuration.LocalConfigurationData{
				Agent:   configuration.LocalAgentConfig{License: "token"},
				Storage: []configuration.StorageConfig{{Name: "http"}},
			}, nil
		}
		d.loadConfig = withLicense
		expectLevel(t, runDoctorChecks(d), "config.license", levelOK)
		d.licenseExpiry = func(string) (string, time.Time, error) { return "pro", doctorNow.Add(10 * 24 * time.Hour), nil }
		expectLevel(t, runDoctorChecks(d), "config.license", levelWarn)
		d.licenseExpiry = func(string) (string, time.Time, error) { return "pro", doctorNow.Add(-24 * time.Hour), nil }
		expectLevel(t, runDoctorChecks(d), "config.license", levelFail)
	})
	t.Run("port held by someone else", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.status = func() statusResult { return statusResult{serviceState: "stopped", notRunning: true} }
		d.portFree = func(string, int) error { return errors.New("port 8080 is not available") }
		expectLevel(t, runDoctorChecks(d), "config.http_port", levelFail)
	})
	t.Run("port held by this agent", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.portFree = func(string, int) error { return errors.New("in use") }
		c := expectLevel(t, runDoctorChecks(d), "config.http_port", levelOK)
		if !strings.Contains(c.Message, "running agent") {
			t.Errorf("message %q", c.Message)
		}
	})
}

func TestDoctorOutputsSection(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.outputTest = func(context.Context, string, map[string]interface{}, string, int) []otlp.ConnectionStep {
			return []otlp.ConnectionStep{{Name: "dns", Passed: true}, {Name: "tcp", Passed: false, Error: "connection refused"}}
		}
		c := expectLevel(t, runDoctorChecks(d), "outputs.otlp", levelFail)
		if !strings.Contains(c.Message, "connection refused") || !strings.Contains(c.Message, "tcp") {
			t.Errorf("message %q does not name the step", c.Message)
		}
	})
	t.Run("receives the configured parameters", func(t *testing.T) {
		d := healthyDoctorDeps()
		var gotType, gotEndpoint string
		d.outputTest = func(_ context.Context, typ string, params map[string]interface{}, _ string, _ int) []otlp.ConnectionStep {
			gotType = typ
			gotEndpoint, _ = params["endpoint"].(string)
			return []otlp.ConnectionStep{{Name: "dns", Passed: true}}
		}
		runDoctorChecks(d)
		if gotType != "otlp" || gotEndpoint != "collector:4317" {
			t.Errorf("tested %q with endpoint %q", gotType, gotEndpoint)
		}
	})
	t.Run("the http output is not probed", func(t *testing.T) {
		expectLevel(t, runDoctorChecks(healthyDoctorDeps()), "outputs.http", levelSkip)
	})
	t.Run("output failing inside the running agent", func(t *testing.T) {
		d := healthyDoctorDeps()
		st := runningStatus()
		st.system.StrategyFailures = []status.StrategyFailure{{Strategy: "zabbix", Reason: "bad config", Detail: "server missing"}}
		d.status = func() statusResult { return st }
		c := expectLevel(t, runDoctorChecks(d), "outputs.running", levelFail)
		if !strings.Contains(c.Message, "zabbix") {
			t.Errorf("message %q", c.Message)
		}
	})
	t.Run("no output configured", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.loadConfig = func(string) (configuration.LocalConfigurationData, error) {
			return configuration.LocalConfigurationData{}, nil
		}
		expectLevel(t, runDoctorChecks(d), "outputs.test", levelSkip)
	})
}

func TestDoctorProbesSection(t *testing.T) {
	st := runningStatus()
	st.system.Probes = []status.ProbeStatus{
		{Name: "cpu", Status: "active", MetricsCount: 12, LastUpdate: doctorNow.Add(-2 * time.Minute)},
		{Name: "mysql", Status: "error", LastError: "access denied", LastUpdate: doctorNow.Add(-time.Minute)},
		{Name: "nginx", Status: "inactive"},
		{Name: "redis", Status: "active", LastError: "timeout", MetricsCount: 3, LastUpdate: doctorNow},
	}
	d := healthyDoctorDeps()
	d.status = func() statusResult { return st }
	checks := runDoctorChecks(d)

	c := expectLevel(t, checks, "probes.cpu", levelOK)
	if !strings.Contains(c.Message, "2 min ago") {
		t.Errorf("message %q does not give the last cycle", c.Message)
	}
	c = expectLevel(t, checks, "probes.mysql", levelFail)
	if !strings.Contains(c.Message, "access denied") {
		t.Errorf("message %q", c.Message)
	}
	expectLevel(t, checks, "probes.nginx", levelWarn)
	expectLevel(t, checks, "probes.redis", levelWarn)
}

func TestDoctorAgentAnsweringWithoutProbes(t *testing.T) {
	st := runningStatus()
	st.system.Probes = nil
	d := healthyDoctorDeps()
	d.status = func() statusResult { return st }
	expectLevel(t, runDoctorChecks(d), "probes.status", levelWarn)
}

func TestDoctorHostSection(t *testing.T) {
	cases := []struct {
		free  float64
		level string
	}{{55, levelOK}, {10, levelOK}, {9.9, levelWarn}, {2, levelWarn}, {1.9, levelFail}}
	for _, tc := range cases {
		d := healthyDoctorDeps()
		free := tc.free
		d.freePercent = func(string) (float64, error) { return free, nil }
		expectLevel(t, runDoctorChecks(d), "host.disk", tc.level)
	}

	t.Run("falls back to the config volume", func(t *testing.T) {
		d := healthyDoctorDeps()
		var asked []string
		d.freePercent = func(p string) (float64, error) {
			asked = append(asked, p)
			if p == d.logDir {
				return 0, errors.New("no such directory")
			}
			return 50, nil
		}
		expectLevel(t, runDoctorChecks(d), "host.disk", levelOK)
		if len(asked) != 2 || asked[1] != "/etc/senhub-agent" {
			t.Errorf("asked %v", asked)
		}
	})
	t.Run("unreadable volume is skipped", func(t *testing.T) {
		d := healthyDoctorDeps()
		d.freePercent = func(string) (float64, error) { return 0, errors.New("boom") }
		expectLevel(t, runDoctorChecks(d), "host.disk", levelSkip)
	})
	t.Run("memory", func(t *testing.T) {
		c := expectLevel(t, runDoctorChecks(healthyDoctorDeps()), "host.memory", levelOK)
		if !strings.Contains(c.Message, "41 MB") {
			t.Errorf("message %q", c.Message)
		}
	})
}

func TestRunDoctorRejectsUnknownOption(t *testing.T) {
	var out bytes.Buffer
	if code := runDoctor([]string{"--json", "--bogus"}, &out); code != cliexit.Failure {
		t.Errorf("exit code %d", code)
	}
	var doc struct {
		Schema string `json:"schema"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Schema != "senhub.cli.doctor/v1" || doc.Error == "" {
		t.Errorf("not a doctor failure document: %v %s", err, out.String())
	}
}

func TestDoctorNeedsNoPrivileges(t *testing.T) {
	if !readOnlyCommand([]string{"agent", "doctor", "--json"}) {
		t.Error("doctor must run without root: it reports what it cannot check")
	}
	if _, ok := knownTopLevelArgs["doctor"]; !ok {
		t.Error("doctor is not a known command")
	}
}

func TestReadSecretStatusWithoutStoreCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	rep, err := readSecretStatus(dir)
	if err != nil || rep.Present {
		t.Fatalf("got %+v, %v", rep, err)
	}
	if secretStorePresent(dir) {
		t.Error("looking for a store created one")
	}
}

func TestStatusExitCodeWarnsOnProbeInError(t *testing.T) {
	r := runningStatus()
	r.system.Health.Status = "healthy"
	if code := r.exitCode(); code != cliexit.OK {
		t.Fatalf("healthy agent: exit code %d, want %d", code, cliexit.OK)
	}
	r.system.Probes = append(r.system.Probes, status.ProbeStatus{Name: "nginx-logs", Status: "error", LastError: "permission denied"})
	if code := r.exitCode(); code != cliexit.Warning {
		t.Errorf("a probe in error: exit code %d, want %d", code, cliexit.Warning)
	}
	r = runningStatus()
	r.system.Health.Status = "healthy"
	r.system.ProbesError = "decoding /info/probes: unexpected shape"
	if code := r.exitCode(); code != cliexit.Warning {
		t.Errorf("unreadable probe list: exit code %d, want %d", code, cliexit.Warning)
	}
}

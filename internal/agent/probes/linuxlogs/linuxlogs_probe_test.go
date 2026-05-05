package linuxlogs

import (
	"bufio"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func bufioReaderFromString(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func testLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "test")
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Priority != DefaultPriority {
		t.Errorf("Priority=%d, want %d", cfg.Priority, DefaultPriority)
	}
	if len(cfg.Units) != 0 || len(cfg.Identifiers) != 0 {
		t.Errorf("filters not empty: units=%v ids=%v", cfg.Units, cfg.Identifiers)
	}
	if cfg.IncludeBoot {
		t.Errorf("IncludeBoot should default to false")
	}
}

func TestParseConfig_AllFields(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"units":        []interface{}{"ssh.service", "nginx.service"},
		"identifiers":  []interface{}{"sshd", "kernel"},
		"priority":     float64(4),
		"include_boot": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Priority != 4 {
		t.Errorf("Priority=%d", cfg.Priority)
	}
	if len(cfg.Units) != 2 || cfg.Units[0] != "ssh.service" {
		t.Errorf("Units=%v", cfg.Units)
	}
	if len(cfg.Identifiers) != 2 || cfg.Identifiers[1] != "kernel" {
		t.Errorf("Identifiers=%v", cfg.Identifiers)
	}
	if !cfg.IncludeBoot {
		t.Errorf("IncludeBoot=false")
	}
}

func TestParseConfig_RejectsBadPriority(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{"priority": float64(99)})
	if err == nil || !strings.Contains(err.Error(), "priority") {
		t.Errorf("expected priority error, got: %v", err)
	}
}

func TestBuildJournalctlArgs_DefaultsSinceNow(t *testing.T) {
	args := buildJournalctlArgs(LinuxLogsProbeConfig{Priority: DefaultPriority})

	requires := []string{"--output=json", "--no-pager", "--follow", "--since=now", "--priority=7"}
	for _, want := range requires {
		if !contains(args, want) {
			t.Errorf("missing %q in args: %v", want, args)
		}
	}
}

func TestBuildJournalctlArgs_OmitsSinceWhenIncludeBoot(t *testing.T) {
	args := buildJournalctlArgs(LinuxLogsProbeConfig{Priority: DefaultPriority, IncludeBoot: true})
	if contains(args, "--since=now") {
		t.Errorf("--since=now leaked when IncludeBoot=true: %v", args)
	}
}

func TestBuildJournalctlArgs_FiltersAndPriority(t *testing.T) {
	args := buildJournalctlArgs(LinuxLogsProbeConfig{
		Priority:    3,
		Units:       []string{"ssh.service"},
		Identifiers: []string{"kernel"},
	})
	for _, want := range []string{"--unit=ssh.service", "--identifier=kernel", "--priority=3"} {
		if !contains(args, want) {
			t.Errorf("missing %q: %v", want, args)
		}
	}
}

func TestParseRealtime_RoundTrip(t *testing.T) {
	// Microseconds since epoch as per __REALTIME_TIMESTAMP.
	got := parseRealtime("1700000000123456")
	want := time.Unix(1700000000, 123456000)
	if !got.Equal(want) {
		t.Errorf("got=%v want=%v", got, want)
	}
}

func TestParseRealtime_FallsBackOnGarbage(t *testing.T) {
	before := time.Now().Add(-time.Second)
	got := parseRealtime("not-a-number")
	after := time.Now().Add(time.Second)
	if got.Before(before) || got.After(after) {
		t.Errorf("garbage timestamp didn't fall back to now: %v", got)
	}
}

func TestParseEntry_PopulatesFields(t *testing.T) {
	entry := journalEntry{
		Priority:         "4", // warning
		Message:          "thing happened",
		Hostname:         "edge-01",
		SystemdUnit:      "ssh.service",
		SyslogIdentifier: "sshd",
		PID:              "4321",
		RealtimeUS:       "1700000000000000",
	}
	got := parseEntry(entry, "linux-logs-1")
	if got.Body != "thing happened" {
		t.Errorf("Body=%q", got.Body)
	}
	if got.Severity != agentstate.LogSeverityWarn {
		t.Errorf("Severity=%d", got.Severity)
	}
	if got.SeverityText != "WARN" {
		t.Errorf("SeverityText=%q", got.SeverityText)
	}
	if got.Attributes["host.name"] != "edge-01" {
		t.Errorf("host.name=%q", got.Attributes["host.name"])
	}
	if got.Attributes["systemd.unit"] != "ssh.service" {
		t.Errorf("systemd.unit=%q", got.Attributes["systemd.unit"])
	}
	if got.Attributes["syslog.appname"] != "sshd" {
		t.Errorf("syslog.appname=%q", got.Attributes["syslog.appname"])
	}
	if got.Attributes["process.pid"] != "4321" {
		t.Errorf("process.pid=%q", got.Attributes["process.pid"])
	}
	if got.ProducerProbeName != "linux-logs-1" || got.ProducerProbeType != "linux_logs" {
		t.Errorf("producer identity wrong: name=%q type=%q",
			got.ProducerProbeName, got.ProducerProbeType)
	}
	want := time.Unix(1700000000, 0)
	if !got.Timestamp.Equal(want) {
		t.Errorf("Timestamp=%v want %v", got.Timestamp, want)
	}
}

func TestDrainReader_ParsesAndPublishes(t *testing.T) {
	// Use the real publish path; verify by subscribing.
	ch := agentstate.SubscribeLogs(8)
	defer agentstate.UnsubscribeLogs(ch)

	// Two valid lines + one malformed line in between.
	input := `{"PRIORITY":"6","MESSAGE":"hi","_HOSTNAME":"h","__REALTIME_TIMESTAMP":"1700000000000000"}
not json at all
{"PRIORITY":"3","MESSAGE":"boom","_HOSTNAME":"h","__REALTIME_TIMESTAMP":"1700000001000000"}
`
	r := bufioReaderFromString(input)
	drainReader(r, testLogger(), "linux-logs-test")

	// Drain everything that was published.
	deadline := time.After(2 * time.Second)
	got := []agentstate.LogRecord{}
loop:
	for {
		select {
		case rec := <-ch:
			got = append(got, rec)
		case <-deadline:
			break loop
		default:
			break loop
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (the two valid lines)", len(got))
	}
	if got[0].Body != "hi" || got[1].Body != "boom" {
		t.Errorf("bodies=%q,%q", got[0].Body, got[1].Body)
	}
	if got[1].Severity != agentstate.LogSeverityError {
		t.Errorf("second record severity=%d, want ERROR (17)", got[1].Severity)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

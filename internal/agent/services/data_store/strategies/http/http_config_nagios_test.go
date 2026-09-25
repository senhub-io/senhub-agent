package http

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestLoadNagiosConfig_DefaultIsEmbedded asserts that with no operator
// file present, the agent serves the curated embedded configuration —
// not the minimal hardcoded fallback whose historical channel names
// (cpu_usage_percent, network_bytes_total) no probe emits (#315).
func TestLoadNagiosConfig_DefaultIsEmbedded(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())

	config := cm.LoadNagiosConfig()
	if config == nil {
		t.Fatal("LoadNagiosConfig returned nil")
	}
	if config.Description == "Fallback Nagios configuration" {
		t.Fatalf("default Nagios config is the hardcoded fallback (version %s); expected the embedded definitions/nagios.yaml", config.Version)
	}
	if len(config.Checks) < 2 {
		t.Fatalf("embedded Nagios config has %d checks, expected the curated set", len(config.Checks))
	}
}

// TestNagiosDefaultChecks_ChannelsExist asserts that every channel
// referenced by the served default Nagios configuration AND by the
// hardcoded fallback exists in the transformer probe definitions —
// i.e. some probe actually emits it. A check referencing a phantom
// channel returns permanent UNKNOWN (#315).
func TestNagiosDefaultChecks_ChannelsExist(t *testing.T) {
	perProbe, err := transformers.DefinitionMetricNames()
	if err != nil {
		t.Fatalf("DefinitionMetricNames: %v", err)
	}
	emitted := make(map[string]bool)
	for _, names := range perProbe {
		for _, n := range names {
			emitted[n] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("no metric names extracted from embedded probe definitions")
	}

	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())

	configs := map[string]*NagiosConfig{
		"served-default":     cm.LoadNagiosConfig(),
		"hardcoded-fallback": cm.createFallbackNagiosConfig(),
	}
	for source, config := range configs {
		for _, check := range config.Checks {
			for _, metric := range check.Metrics {
				if !emitted[metric.Channel] {
					t.Errorf("%s: check %q references channel %q which no probe definition emits", source, check.Name, metric.Channel)
				}
			}
		}
	}
}

func writeNagiosFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "nagios.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An operator's nagios.yaml is read from the directory that holds the
// agent configuration: the working directory of a service is
// /usr/local/bin or the install folder, never a place to write to.
func TestLoadNagiosConfig_ReadsFileNextToAgentConfig(t *testing.T) {
	dir := t.TempDir()
	writeNagiosFile(t, dir, `version: "site-1"
checks:
  - name: cpu_user_check
    metrics:
      - channel: cpu_user
        warning: "80"
        critical: "90"
`)
	cm := NewConfigurationManager(pathedConfig{path: filepath.Join(dir, "agent.yaml")}, map[string]interface{}{}, newTestLogger())

	config := cm.LoadNagiosConfig()
	if config.Version != "site-1" || len(config.Checks) != 1 || config.Checks[0].Name != "cpu_user_check" {
		t.Fatalf("expected the operator file next to agent.yaml, got version %q with %d checks", config.Version, len(config.Checks))
	}
}

func TestLoadNagiosConfig_InvalidFileFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	writeNagiosFile(t, dir, "version: \"broken\"\nchecks: []\n")
	cm := NewConfigurationManager(pathedConfig{path: filepath.Join(dir, "agent.yaml")}, map[string]interface{}{}, newTestLogger())

	config := cm.LoadNagiosConfig()
	if config.Version == "broken" {
		t.Fatal("a file with no check was served")
	}
	if len(config.Checks) < 2 {
		t.Fatalf("expected the embedded curated checks, got %d", len(config.Checks))
	}
}

// A check matches the metric name. A PRTG label is the usual mistake,
// and the loader names the metric to use instead; a metric of another
// operating system is reported as such.
func TestUndeclaredNagiosChannels(t *testing.T) {
	config := &NagiosConfig{Checks: []NagiosCheck{{
		Name: "cpu",
		Metrics: []NagiosMetric{
			{Channel: "cpu_user"},
			{Channel: "CPU User"},
			{Channel: "cpu_user_time"},
			{Channel: "disk_free_percent"},
			{Channel: "no_such_metric"},
		},
	}}}

	got, err := undeclaredNagiosChannels(config, "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := []undeclaredNagiosChannel{
		{Check: "cpu", Channel: "CPU User", Hint: "cpu_user"},
		{Check: "cpu", Channel: "cpu_user_time", Hint: "cpu_user"},
		{Check: "cpu", Channel: "disk_free_percent", Platforms: []string{"windows"}},
		{Check: "cpu", Channel: "no_such_metric"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// What would otherwise surface as a permanent UNKNOWN, or be dropped
// without a word, is refused at load with the check it belongs to.
func TestValidateNagiosConfigRefusesWhatWouldSilentlyMisbehave(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"misspelt key": {`version: "1"
checks:
  - name: c
    metrics:
      - channel: cpu_user
        warning: "80"
        critcal: "90"
`, "critcal"},
		"threshold not a number": {`version: "1"
checks:
  - name: c
    metrics:
      - channel: cpu_user
        warning: "80%"
`, `warning threshold "80%" is not a number`},
		"unknown aggregation": {`version: "1"
checks:
  - name: c
    metrics:
      - channel: cpu_user
        warning: "80"
        aggregation: median
`, `unknown aggregation "median"`},
		"unknown operator": {`version: "1"
checks:
  - name: c
    tag_filters:
      - key: core
        operator: contains
        values: ["0"]
    metrics:
      - channel: cpu_user
        warning: "80"
`, `unknown operator "contains"`},
		"per-series thresholds on an aggregate": {`version: "1"
checks:
  - name: c
    metrics:
      - channel: cpu_core_usage
        warning: "80"
        aggregation: max
        tag_specific_thresholds:
          - tags: {core: "0"}
            warning: "50"
`, "need aggregation none"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeNagiosFile(t, dir, tc.body)
			cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
			_, err := cm.loadNagiosConfigFromFile(filepath.Join(dir, "nagios.yaml"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// The example on the user-guide Nagios page loads as written and names
// only metrics a Linux agent emits, the platform the example is written
// for, so a reader who copies it gets checks that answer.
func TestNagiosPageExampleLoads(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "docs", "user-guide", "docs", "nagios.md"))
	if err != nil {
		t.Fatal(err)
	}
	var example string
	text := strings.ReplaceAll(string(page), "\r\n", "\n") // a Windows checkout is CRLF
	for _, block := range strings.Split(text, "```yaml\n")[1:] {
		body := block[:strings.Index(block, "```")]
		if strings.Contains(body, "checks:") {
			example = body
			break
		}
	}
	if example == "" {
		t.Fatal("no nagios.yaml example on the page")
	}

	dir := t.TempDir()
	writeNagiosFile(t, dir, example)
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	config, err := cm.loadNagiosConfigFromFile(filepath.Join(dir, "nagios.yaml"))
	if err != nil {
		t.Fatalf("the page's example does not load: %v", err)
	}
	undeclared, err := undeclaredNagiosChannels(config, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(undeclared) > 0 {
		t.Errorf("the page's example names metrics no probe emits: %+v", undeclared)
	}
}

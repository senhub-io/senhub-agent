package configuration

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestLogsConfigParsesAndValidates(t *testing.T) {
	const doc = `
logs:
  redact:
    preset: false
    rules:
      - name: card
        pattern: '(\b\d{4})\d{8}(\d{4}\b)'
        replacement: '$1********$2'
      - name: broken
        pattern: '('
        replacement: x
`
	var data LocalConfigurationData
	if err := yaml.Unmarshal([]byte(doc), &data); err != nil {
		t.Fatal(err)
	}
	preset, rules := data.Logs.redactSettings()
	if preset || len(rules) != 2 {
		t.Fatalf("preset=%v rules=%d, want false and 2", preset, len(rules))
	}
	errs := ValidateLogsConfig(data.Logs)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `"broken"`) {
		t.Fatalf("errors = %v, want one error naming the broken rule", errs)
	}
}

func TestLogsConfigDefaultsToPreset(t *testing.T) {
	if preset, rules := (*LogsConfig)(nil).redactSettings(); !preset || rules != nil {
		t.Fatalf("nil block: preset=%v rules=%v, want preset on and no rules", preset, rules)
	}
	if preset, _ := (&LogsConfig{Redact: &LogsRedactConfig{}}).redactSettings(); !preset {
		t.Fatal("omitted preset must default to true")
	}
}

func TestApplyLogsConfigSkipsBadRuleAndKeepsOthers(t *testing.T) {
	t.Cleanup(func() { agentstate.ConfigureLogRedaction(true, nil) })
	errs := ApplyLogsConfig(&LogsConfig{Redact: &LogsRedactConfig{Rules: []LogsRedactRuleSpec{
		{Name: "bad", Pattern: "("},
		{Name: "ok", Pattern: "x+", Replacement: "y"},
	}}})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `"bad"`) {
		t.Fatalf("errors = %v, want one error naming bad", errs)
	}
}

func TestLogsChangeIsAConfigurationChange(t *testing.T) {
	zl := zerolog.Nop()
	lc := &LocalConfiguration{logger: logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.local.test")}
	off := false
	with := LocalConfigurationData{Logs: &LogsConfig{Redact: &LogsRedactConfig{Preset: &off}}}
	if !lc.hasConfigurationChanged(LocalConfigurationData{}, with) {
		t.Fatal("a logs-only change must be detected so the rules reload without a restart")
	}
}

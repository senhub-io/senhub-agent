package configuration

import (
	"senhub-agent.go/internal/agent/services/agentstate"
)

// LogsConfig is the top-level `logs:` block.
type LogsConfig struct {
	Redact *LogsRedactConfig `yaml:"redact,omitempty"`
}

// LogsRedactConfig controls the masking of secrets in log records
// before any output receives them. Preset defaults to true when
// omitted; Rules apply after the preset.
type LogsRedactConfig struct {
	Preset *bool                `yaml:"preset,omitempty"`
	Rules  []LogsRedactRuleSpec `yaml:"rules,omitempty"`
}

// LogsRedactRuleSpec is one custom masking rule.
type LogsRedactRuleSpec struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"`
	Replacement string `yaml:"replacement"`
}

func (c *LogsConfig) redactSettings() (preset bool, rules []agentstate.LogRedactRule) {
	preset = true
	if c == nil || c.Redact == nil {
		return preset, nil
	}
	if c.Redact.Preset != nil {
		preset = *c.Redact.Preset
	}
	for _, r := range c.Redact.Rules {
		rules = append(rules, agentstate.LogRedactRule{Name: r.Name, Pattern: r.Pattern, Replacement: r.Replacement})
	}
	return preset, rules
}

// ValidateLogsConfig reports every rule of the block that cannot be
// used, each error naming the rule. Used by `config check`.
func ValidateLogsConfig(c *LogsConfig) []error {
	_, rules := c.redactSettings()
	return agentstate.ValidateLogRedactRules(rules)
}

// ApplyLogsConfig installs the block's masking settings on the log
// rail; a nil block means the default preset. Rules that cannot be
// used are skipped and returned, the others still apply.
func ApplyLogsConfig(c *LogsConfig) []error {
	preset, rules := c.redactSettings()
	return agentstate.ConfigureLogRedaction(preset, rules)
}

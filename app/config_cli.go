package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/cliexit"
)

// checkOutcome is what a configuration check found. loadErr is set when
// the configuration could not be loaded at all, in which case the counts
// are zero.
type checkOutcome struct {
	errors   int
	warnings int
	loadErr  error
}

// exitCode maps the outcome onto the CLI contract: an unreadable or
// invalid configuration fails, a valid one with warnings warns.
func (o checkOutcome) exitCode() int {
	switch {
	case o.loadErr != nil || o.errors > 0:
		return cliexit.Failure
	case o.warnings > 0:
		return cliexit.Warning
	default:
		return cliexit.OK
	}
}

type checkFinding struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type configCheckReport struct {
	jsonHeader
	ConfigPath string         `json:"config_path"`
	Errors     int            `json:"errors"`
	Warnings   int            `json:"warnings"`
	Findings   []checkFinding `json:"findings"`
}

var checkFindingLine = regexp.MustCompile(`^\s*\[(OK|INFO|WARN|ERROR|OFF)\]\s+(.*\S)\s*$`)

// parseCheckFindings turns the text report of checkConfig into one entry
// per status line. Continuation lines (hints, context dumps) belong to
// the line above them and stay in the text view only.
func parseCheckFindings(text string) []checkFinding {
	findings := []checkFinding{}
	for _, line := range strings.Split(text, "\n") {
		m := checkFindingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		findings = append(findings, checkFinding{Level: strings.ToLower(m[1]), Message: m[2]})
	}
	return findings
}

// runConfigCheck implements `config check [--json] [path]` and returns the
// exit code. The text report is the one checkConfig has always printed;
// the JSON view is derived from that same report.
func runConfigCheck(args []string, out io.Writer) int {
	args, jsonMode := extractJSONFlag(args)
	configPath, err := parseConfigPathArgs(args)
	if err != nil {
		return reportFailure("config.check", jsonMode, out, fmt.Errorf("config check: %w", err))
	}
	if resolved, resErr := cliArgs.GetAbsoluteConfigPath(configPath); resErr == nil {
		configPath = resolved
	}

	if !jsonMode {
		return checkConfig(configPath).exitCode()
	}

	var outcome checkOutcome
	text, capErr := captureOutput(func() { outcome = checkConfig(configPath) })
	if capErr != nil {
		return reportFailure("config.check", true, out, fmt.Errorf("config check: %w", capErr))
	}

	code := outcome.exitCode()
	absPath, absErr := filepath.Abs(configPath)
	if absErr != nil {
		absPath = configPath
	}
	report := configCheckReport{
		jsonHeader: newJSONHeader("config.check", code),
		ConfigPath: absPath,
		Errors:     outcome.errors,
		Warnings:   outcome.warnings,
		Findings:   parseCheckFindings(text),
	}
	if outcome.loadErr != nil {
		report.Error = outcome.loadErr.Error()
	}
	if err := writeJSON(out, report); err != nil {
		return reportFailure("config.check", false, out, err)
	}
	return code
}

type configShowReport struct {
	jsonHeader
	ConfigPath string `json:"config_path"`
	Mode       string `json:"mode"`
	Config     any    `json:"config"`
}

// showModeName is the label of a show mode as the flags spell it.
func showModeName(mode configuration.ShowMode) string {
	switch mode {
	case configuration.ShowRaw:
		return "raw"
	case configuration.ShowResolved:
		return "resolved"
	default:
		return "redact"
	}
}

// runConfigShow implements `config show [--raw|--resolved|--redact]
// [--json] [path]` and returns the exit code. It prints the merged
// configuration as YAML for diffability and audit. Modes:
//
//	--redact             the DEFAULT: resolved, but values that came
//	                     from ${file:..} OR sit under a YAML key whose
//	                     name matches (?i)(key|token|password|secret)
//	                     are masked with "***". Safe for tickets.
//	--resolved           resolved WITHOUT redaction: secrets in
//	                     cleartext. Requires the explicit flag, because
//	                     operators paste config show into tickets (#279).
//	--raw                references preserved as written.
//
// The YAML keys are sorted so two runs produce byte-identical output.
// --json wraps the same data, redacted the same way, under "config".
func runConfigShow(args []string, out io.Writer) int {
	args, jsonMode := extractJSONFlag(args)
	mode := configuration.ShowRedact
	configPath := ""

	for _, a := range args {
		switch a {
		case "--raw":
			mode = configuration.ShowRaw
		case "--resolved":
			mode = configuration.ShowResolved
		case "--redact":
			mode = configuration.ShowRedact
		case "-h", "--help":
			fmt.Fprintln(out, "Usage: agent config show [--raw|--resolved|--redact] [--json] [path]")
			return cliexit.OK
		default:
			if strings.HasPrefix(a, "--") {
				return reportFailure("config.show", jsonMode, out, fmt.Errorf("config show: unknown flag %q", a))
			}
			configPath = a
		}
	}

	// Resolve to the OS-canonical absolute path when no explicit path
	// was given, mirroring what `agent run` / `agent start` use.
	if resolved, err := cliArgs.GetAbsoluteConfigPath(configPath); err == nil {
		configPath = resolved
	} else if absPath, absErr := filepath.Abs(configPath); absErr == nil {
		configPath = absPath
	}

	// The loader WARNs about legacy detection and duplicate strategies;
	// those go to stderr so stdout carries the document alone.
	zlog := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	base := (*agentLogger.Logger)(&zlog)
	log := agentLogger.NewModuleLogger(base, "configuration.show")

	data, err := configuration.LoadForShow(configPath, mode, log)
	if err != nil {
		return reportFailure("config.show", jsonMode, out, fmt.Errorf("config show: %w", err))
	}

	yamlOut, err := configuration.MarshalSortedYAML(&data)
	if err != nil {
		return reportFailure("config.show", jsonMode, out, fmt.Errorf("config show: marshaling output: %w", err))
	}

	if !jsonMode {
		if _, err := out.Write(yamlOut); err != nil {
			return reportFailure("config.show", false, out, fmt.Errorf("config show: write: %w", err))
		}
		return cliexit.OK
	}

	var tree any
	if err := yaml.Unmarshal(yamlOut, &tree); err != nil {
		return reportFailure("config.show", true, out, fmt.Errorf("config show: reading back the configuration: %w", err))
	}
	report := configShowReport{
		jsonHeader: newJSONHeader("config.show", cliexit.OK),
		ConfigPath: configPath,
		Mode:       showModeName(mode),
		Config:     jsonSafeTree(tree),
	}
	if err := writeJSON(out, report); err != nil {
		return reportFailure("config.show", false, out, err)
	}
	return cliexit.OK
}

// jsonSafeTree rewrites a decoded YAML tree so encoding/json accepts it:
// maps keyed by anything other than a string are re-keyed with their
// printed form.
func jsonSafeTree(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = jsonSafeTree(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = jsonSafeTree(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = jsonSafeTree(val)
		}
		return out
	default:
		return v
	}
}

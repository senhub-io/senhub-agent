package app

import (
	"fmt"
	"io"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/cliexit"
)

const doctorCommand = "doctor"

const (
	levelOK   = "ok"
	levelWarn = "warn"
	levelFail = "fail"
	levelSkip = "skip"
)

const (
	sectionInstall       = "install"
	sectionConfiguration = "configuration"
	sectionOutputs       = "outputs"
	sectionProbes        = "probes"
	sectionHost          = "host"
)

var doctorSections = []struct{ id, title string }{
	{sectionInstall, "Install"},
	{sectionConfiguration, "Configuration"},
	{sectionOutputs, "Outputs"},
	{sectionProbes, "Probes"},
	{sectionHost, "Host"},
}

// doctorCheck is one finding. Fix is the command or action that clears a
// check that is not OK, and is empty otherwise.
type doctorCheck struct {
	Section string `json:"section"`
	ID      string `json:"id"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

type doctorSummary struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

type doctorReport struct {
	jsonHeader
	ConfigPath string        `json:"config_path"`
	Summary    doctorSummary `json:"summary"`
	Checks     []doctorCheck `json:"checks"`
}

func okCheck(section, id, msg string) doctorCheck {
	return doctorCheck{Section: section, ID: id, Level: levelOK, Message: msg}
}

func warnCheck(section, id, msg, fix string) doctorCheck {
	return doctorCheck{Section: section, ID: id, Level: levelWarn, Message: msg, Fix: fix}
}

func failCheck(section, id, msg, fix string) doctorCheck {
	return doctorCheck{Section: section, ID: id, Level: levelFail, Message: msg, Fix: fix}
}

func skipCheck(section, id, msg string) doctorCheck {
	return doctorCheck{Section: section, ID: id, Level: levelSkip, Message: msg}
}

func summarize(checks []doctorCheck) doctorSummary {
	var s doctorSummary
	for _, c := range checks {
		switch c.Level {
		case levelOK:
			s.OK++
		case levelWarn:
			s.Warn++
		case levelFail:
			s.Fail++
		case levelSkip:
			s.Skip++
		}
	}
	return s
}

// doctorExitCode: a failure outranks a warning; a skipped check says
// nothing about health and counts for neither.
func doctorExitCode(checks []doctorCheck) int {
	s := summarize(checks)
	switch {
	case s.Fail > 0:
		return cliexit.Failure
	case s.Warn > 0:
		return cliexit.Warning
	default:
		return cliexit.OK
	}
}

// runDoctorChecks runs every section against d, in display order. A
// section never stops the next one: the point of the command is to show
// everything that is wrong at once.
func runDoctorChecks(d doctorDeps) []doctorCheck {
	cfg := loadDoctorConfig(d)
	st := d.status()

	var checks []doctorCheck
	checks = append(checks, checkInstall(d, cfg, st)...)
	checks = append(checks, checkConfiguration(d, cfg, st)...)
	checks = append(checks, checkOutputs(d, cfg, st)...)
	checks = append(checks, checkProbes(d, st)...)
	checks = append(checks, checkHost(d, st)...)
	return checks
}

func renderDoctorText(out io.Writer, configPath string, checks []doctorCheck) {
	fmt.Fprintf(out, "Diagnosing the agent (configuration: %s)\n", configPath)
	for _, sec := range doctorSections {
		var rows []doctorCheck
		for _, c := range checks {
			if c.Section == sec.id {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(out, "\n%s\n", sec.title)
		for _, c := range rows {
			fmt.Fprintf(out, "  %-6s %s: %s\n", "["+strings.ToUpper(c.Level)+"]", c.ID, c.Message)
			if c.Fix != "" {
				fmt.Fprintf(out, "         fix: %s\n", c.Fix)
			}
		}
	}
	s := summarize(checks)
	fmt.Fprintf(out, "\n%d ok, %d warning, %d failure, %d skipped\n", s.OK, s.Warn, s.Fail, s.Skip)
}

// runDoctor implements `doctor [--json] [--config-path <path>]` and
// returns the exit code.
func runDoctor(args []string, out io.Writer) int {
	args, jsonMode := extractJSONFlag(args)
	configPath, err := parseConfigPathArgs(args)
	if err != nil {
		return reportFailure(doctorCommand, jsonMode, out, fmt.Errorf("doctor: %w", err))
	}
	resolved, err := cliArgs.GetAbsoluteConfigPath(configPath)
	if err != nil {
		return reportFailure(doctorCommand, jsonMode, out, fmt.Errorf("doctor: resolving the config path: %w", err))
	}
	return executeDoctor(newDoctorDeps(resolved), jsonMode, out)
}

func executeDoctor(d doctorDeps, jsonMode bool, out io.Writer) int {
	checks := runDoctorChecks(d)
	code := doctorExitCode(checks)
	if !jsonMode {
		renderDoctorText(out, d.configPath, checks)
		return code
	}
	report := doctorReport{
		jsonHeader: newJSONHeader(doctorCommand, code),
		ConfigPath: d.configPath,
		Summary:    summarize(checks),
		Checks:     checks,
	}
	if report.Checks == nil {
		report.Checks = []doctorCheck{}
	}
	if err := writeJSON(out, report); err != nil {
		return reportFailure(doctorCommand, false, out, err)
	}
	return code
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

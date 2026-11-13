package app

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/extension"
)

var checkLinePrefix = map[string]string{
	extension.CheckOK:    "  [OK]   ",
	extension.CheckOff:   "  [OFF]  ",
	extension.CheckWarn:  "  [WARN] ",
	extension.CheckError: "  [ERROR] ",
}

// reportExtensions prints the `config check` lines of each registered
// extension for the block it claims.
func reportExtensions(data configuration.LocalConfigurationData) (errors, warnings int) {
	for _, e := range extension.Registered() {
		if e.Check == nil {
			continue
		}
		var block map[string]any
		present := false
		if e.ConfigBlock != "" {
			block, present = configuration.ExtensionBlockOf(data, e.ConfigBlock)
		}
		for _, line := range e.Check(block, present) {
			prefix, known := checkLinePrefix[line.Level]
			if !known {
				prefix = checkLinePrefix[extension.CheckWarn]
			}
			fmt.Printf("%s%s\n", prefix, line.Text)
			switch line.Level {
			case extension.CheckError:
				errors++
			case extension.CheckOK, extension.CheckOff:
			default:
				warnings++
			}
		}
	}
	return errors, warnings
}

// extensionChecks asks each registered extension for its doctor line,
// with what the running agent published for it.
func extensionChecks(cfg doctorConfig, st statusResult) []doctorCheck {
	var out []doctorCheck
	for _, e := range extension.Registered() {
		if e.Doctor == nil {
			continue
		}
		in := extension.DoctorInput{ConfigErr: cfg.err, AgentAnswered: daemonAnswered(st)}
		if cfg.err == nil && e.ConfigBlock != "" {
			in.Block, in.Present = configuration.ExtensionBlockOf(cfg.data, e.ConfigBlock)
		}
		if in.AgentAnswered {
			in.Status = st.system.Extensions[e.Name]
		}
		line := e.Doctor(in)
		id := line.ID
		if id == "" {
			id = "config." + e.Name
		}
		switch line.Level {
		case extension.DoctorOK:
			out = append(out, okCheck(sectionConfiguration, id, line.Message))
		case extension.DoctorWarn:
			out = append(out, warnCheck(sectionConfiguration, id, line.Message, line.Fix))
		case extension.DoctorFail:
			out = append(out, failCheck(sectionConfiguration, id, line.Message, line.Fix))
		default:
			out = append(out, skipCheck(sectionConfiguration, id, line.Message))
		}
	}
	return out
}

package app

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// unitTemplateFuncs are the two helpers kardianos/service gives the unit
// templates; rendering with the same ones is what makes a comparison with
// the installed file meaningful.
var unitTemplateFuncs = template.FuncMap{
	"cmd":       func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` },
	"cmdEscape": func(s string) string { return strings.ReplaceAll(s, " ", `\x20`) },
}

// renderInstallUnit renders the unit install would write: the template the
// installer hands to kardianos/service, with the executable, arguments and
// working directory it resolved.
func renderInstallUnit(script, execPath string, args []string, workingDir string) (string, error) {
	tmpl, err := template.New("unit").Funcs(unitTemplateFuncs).Parse(script)
	if err != nil {
		return "", fmt.Errorf("parsing unit template: %w", err)
	}
	var out bytes.Buffer
	data := struct {
		Path             string
		Arguments        []string
		WorkingDirectory string
	}{execPath, args, workingDir}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("rendering unit: %w", err)
	}
	return out.String(), nil
}

// desiredInstallUnit is the unit a Linux install leaves on disk for
// serviceUser: ExecStart at the system binary, whatever the invocation path.
func desiredInstallUnit(serviceUser string, serviceArgs []string) (string, error) {
	return renderInstallUnit(linuxSystemdScript(serviceUser), systemBinaryUnitPath(), serviceArgs, systemBinaryDir)
}

// unitMatches reports whether the installed unit already is what install
// would leave there. The operator's own [Service] directives (and the
// comment withPreservedDirectives adds above them) are set aside before
// comparing, as refresh-unit keeps them (#826): an added EnvironmentFile=
// is not drift.
func unitMatches(installed, desired string) bool {
	managed := serviceDirectiveKeys(desired)
	for _, decided := range []string{"ExecStart", "WorkingDirectory", "User", "Group"} {
		managed[decided] = true
	}
	for _, retired := range retiredUnitDirectives {
		managed[retired] = true
	}
	return strings.Join(managedUnitLines(installed, managed), "\n") == strings.Join(managedUnitLines(desired, managed), "\n")
}

// managedUnitLines is the unit without blank lines, the preserved-directives
// comment and the [Service] directives outside managed.
func managedUnitLines(unit string, managed map[string]bool) []string {
	var out []string
	inService := false
	for _, line := range strings.Split(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "", trimmed == preservedDirectivesComment:
			continue
		case strings.HasPrefix(trimmed, "["):
			inService = trimmed == "[Service]"
		case inService && !strings.HasPrefix(trimmed, "#"):
			if key, ok := directiveKey(trimmed); ok && !managed[key] {
				continue
			}
		}
		out = append(out, trimmed)
	}
	return out
}

const preservedDirectivesComment = "# Preserved from the previously installed unit:"

// unitOps is the slice of the host a unit reconciliation touches, injected
// so the decisions are tested without a service manager.
type unitOps struct {
	unitPath     string
	packageOwned bool
	read         func() ([]byte, error)
	write        func(data []byte) error
	reload       func() error
	enabled      func() bool
	enable       func() error
}

// reconcileInstalledService brings an installed service to what install
// defines: the unit rewritten when it drifted, the service enabled when it
// is not. It never restarts anything. The returned list names what was
// changed; empty means the service already was in the requested state.
// A unit owned by the package manager is never rewritten here.
func reconcileInstalledService(ops unitOps, desired string) ([]string, error) {
	var changed []string
	if !ops.packageOwned {
		installed, err := ops.read()
		if err != nil {
			return nil, fmt.Errorf("reading the installed unit %s: %w", ops.unitPath, err)
		}
		if !unitMatches(string(installed), desired) {
			body := withPreservedDirectives(desired, string(installed))
			if err := ops.write([]byte(body)); err != nil {
				return nil, fmt.Errorf("writing the unit %s: %w", ops.unitPath, err)
			}
			if err := ops.reload(); err != nil {
				return nil, fmt.Errorf("reloading the service manager: %w", err)
			}
			changed = append(changed, ops.unitPath)
		}
	}
	if !ops.enabled() {
		if err := ops.enable(); err != nil {
			return changed, fmt.Errorf("enabling the service: %w", err)
		}
		changed = append(changed, "enable "+installServiceName)
	}
	return changed, nil
}

// installServiceName is the name the service is registered under.
const installServiceName = "senhub-agent"

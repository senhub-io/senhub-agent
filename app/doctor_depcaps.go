package app

import (
	"strings"
)

var dependsOnCapabilities = []string{"cap_dac_read_search", "cap_sys_ptrace"}

// ambientCapabilities lists the capabilities a unit and its drop-ins
// grant, lower-cased. An empty AmbientCapabilities= resets the list, as
// systemd does.
func ambientCapabilities(texts ...string) map[string]bool {
	caps := map[string]bool{}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			v, ok := strings.CutPrefix(strings.TrimSpace(line), "AmbientCapabilities=")
			if !ok {
				continue
			}
			fields := strings.Fields(strings.ToLower(v))
			if len(fields) == 0 {
				caps = map[string]bool{}
				continue
			}
			for _, f := range fields {
				caps[f] = true
			}
		}
	}
	return caps
}

// dependsOnCapsCheck tells an operator who turned entities.depends_on on
// that a service account without the two capabilities sees only its own
// connections: without it, "no dependencies" and "not allowed to see them"
// look the same. Nothing is said for a root service or when depends_on is
// off.
func dependsOnCapsCheck(d doctorDeps, cfg doctorConfig, unit string, unitErr error, serviceUser string) *doctorCheck {
	if cfg.err != nil || unitErr != nil {
		return nil
	}
	e := cfg.data.Entities
	if e == nil || e.DependsOn == nil || !e.DependsOn.Enabled {
		return nil
	}
	if serviceUser == rootServiceUser {
		c := okCheck(sectionInstall, "install.depends_on_caps", "the service runs as root: every process's connections are readable")
		return &c
	}
	texts := []string{unit}
	if d.install.dropIns != nil {
		texts = append(texts, d.install.dropIns()...)
	}
	caps := ambientCapabilities(texts...)
	var missing []string
	for _, c := range dependsOnCapabilities {
		if !caps[c] {
			missing = append(missing, strings.ToUpper(c))
		}
	}
	if len(missing) == 0 {
		c := okCheck(sectionInstall, "install.depends_on_caps", "the unit grants CAP_DAC_READ_SEARCH and CAP_SYS_PTRACE: other users' connections are readable")
		return &c
	}
	c := warnCheck(sectionInstall, "install.depends_on_caps",
		"entities.depends_on is on but the unit lacks "+strings.Join(missing, " and ")+": the agent sees only its own connections, so the dependencies of other services are missing",
		"add a drop-in with AmbientCapabilities=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE, or run the service as root")
	return &c
}

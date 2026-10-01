package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/status"
)

const (
	licenseWarnDays  = 30
	diskWarnPercent  = 10.0
	diskFailPercent  = 2.0
	doctorConfigHint = "senhub-agent config check"
)

type doctorConfig struct {
	data configuration.LocalConfigurationData
	err  error
}

func loadDoctorConfig(d doctorDeps) doctorConfig {
	data, err := d.loadConfig(d.configPath)
	return doctorConfig{data: data, err: err}
}

func daemonAnswered(st statusResult) bool {
	return !st.notRunning && st.source == "daemon"
}

func checkInstall(d doctorDeps, cfg doctorConfig, st statusResult) []doctorCheck {
	var out []doctorCheck
	out = append(out, serviceCheck(st))

	unit, unitErr := "", errors.New("not read")
	if d.install.unit != nil {
		unit, unitErr = d.install.unit()
		out = append(out, unitCheck(d, unit, unitErr))
	} else {
		out = append(out, skipCheck(sectionInstall, "install.unit", "the systemd unit is only checked on Linux"))
	}
	serviceUser := ""
	if unitErr == nil {
		serviceUser = installedServiceUser(unit)
	}

	if bin := binaryCheck(d, st); bin != nil {
		out = append(out, *bin)
	}
	out = append(out, skewCheck(d))

	if c := logGroupCheck(d, cfg, serviceUser); c != nil {
		out = append(out, *c)
	}
	out = append(out, logDirCheck(d, serviceUser))
	return out
}

func serviceCheck(st statusResult) doctorCheck {
	switch {
	case st.managerErr != nil && errors.Is(st.managerErr, service.ErrNotInstalled):
		return warnCheck(sectionInstall, "install.service", "the service is not installed", "senhub-agent install")
	case st.managerErr != nil:
		return skipCheck(sectionInstall, "install.service", fmt.Sprintf("no service manager answers here (%v)", st.managerErr))
	case st.notRunning:
		return warnCheck(sectionInstall, "install.service", fmt.Sprintf("the service is %s", st.serviceState), "senhub-agent start")
	default:
		return okCheck(sectionInstall, "install.service", "the service is running")
	}
}

func unitCheck(d doctorDeps, installed string, err error) doctorCheck {
	if err != nil {
		return skipCheck(sectionInstall, "install.unit", fmt.Sprintf("no systemd unit read (%v)", err))
	}
	exists := d.install.binaryExists
	if exists == nil {
		exists = func(string) bool { return true }
	}
	refreshed := refreshedUnit(installed, exists)
	if refreshed == installed {
		return okCheck(sectionInstall, "install.unit", "the systemd unit matches the one this binary ships")
	}
	changed := 0
	for _, l := range diffLines(installed, refreshed) {
		if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "+ ") {
			changed++
		}
	}
	return warnCheck(sectionInstall, "install.unit",
		fmt.Sprintf("the systemd unit differs from the one this binary ships (%d lines)", changed),
		"sudo senhub-agent refresh-unit")
}

func binaryCheck(d doctorDeps, st statusResult) *doctorCheck {
	ip := d.install
	if ip.mainPID == nil || ip.exeLink == nil || ip.sameContents == nil {
		c := skipCheck(sectionInstall, "install.binary", "the running binary is only compared on Linux")
		return &c
	}
	if st.notRunning || st.managerErr != nil {
		c := skipCheck(sectionInstall, "install.binary", "the service is not running, so there is no running binary to compare")
		return &c
	}
	pid, err := ip.mainPID()
	if err != nil {
		c := skipCheck(sectionInstall, "install.binary", fmt.Sprintf("cannot ask systemd for the service process (%v)", err))
		return &c
	}
	if pid == "" || pid == "0" {
		c := skipCheck(sectionInstall, "install.binary", "the service reports no main process")
		return &c
	}
	exe, err := ip.exeLink(pid)
	if err != nil {
		c := skipCheck(sectionInstall, "install.binary",
			fmt.Sprintf("cannot read the binary of process %s (%v); run doctor as root to check it", pid, err))
		return &c
	}
	if strings.HasSuffix(exe, " (deleted)") {
		c := failCheck(sectionInstall, "install.binary",
			fmt.Sprintf("the service runs a binary that no longer exists (%s)", exe), "sudo senhub-agent restart")
		return &c
	}
	same, err := ip.sameContents(exe, "/proc/"+pid+"/exe")
	if err != nil {
		c := skipCheck(sectionInstall, "install.binary", fmt.Sprintf("cannot compare the running binary with %s (%v)", exe, err))
		return &c
	}
	if !same {
		c := failCheck(sectionInstall, "install.binary",
			fmt.Sprintf("the binary on disk (%s) is not the one the service runs", exe), "sudo senhub-agent restart")
		return &c
	}
	c := okCheck(sectionInstall, "install.binary", fmt.Sprintf("the service runs the binary on disk (%s)", exe))
	return &c
}

func skewCheck(d doctorDeps) doctorCheck {
	ip := d.install
	if ip.serviceBin == nil || ip.binVersion == nil {
		return skipCheck(sectionInstall, "install.skew", "no service copy of the binary to compare")
	}
	bin := ip.serviceBin()
	if bin == "" {
		return okCheck(sectionInstall, "install.skew", "there is no separate service copy of the binary to drift")
	}
	version := ip.binVersion(bin)
	if serviceBinarySkewNote(d.cliVersion, version, bin) != "" {
		return warnCheck(sectionInstall, "install.skew",
			fmt.Sprintf("the service binary %s is %s, this command is %s", bin, version, d.cliVersion),
			"sudo senhub-agent update")
	}
	return okCheck(sectionInstall, "install.skew", fmt.Sprintf("the service binary %s is on this version", bin))
}

var varLogProbeTypes = map[string]bool{"filetail": true, "syslog": true, "linuxlogs": true}

// probesReadingVarLog names the probes configured on a system log under
// /var/log, the files the adm group owns on Debian and Ubuntu.
func probesReadingVarLog(probes []configuration.ProbeConfig) []string {
	var names []string
	for _, p := range probes {
		if varLogProbeTypes[p.Type] && referencesVarLog(p.Params) {
			names = append(names, p.Name)
		}
	}
	return names
}

func referencesVarLog(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.HasPrefix(t, "/var/log/") && !strings.HasPrefix(t, "/var/log/senhub")
	case map[string]interface{}:
		for _, x := range t {
			if referencesVarLog(x) {
				return true
			}
		}
	case map[interface{}]interface{}:
		for _, x := range t {
			if referencesVarLog(x) {
				return true
			}
		}
	case []interface{}:
		for _, x := range t {
			if referencesVarLog(x) {
				return true
			}
		}
	}
	return false
}

func logGroupCheck(d doctorDeps, cfg doctorConfig, serviceUser string) *doctorCheck {
	if d.install.groupMember == nil || cfg.err != nil || serviceUser == "" || serviceUser == rootServiceUser {
		return nil
	}
	probes := probesReadingVarLog(cfg.data.Probes)
	if len(probes) == 0 {
		return nil
	}
	member, exists, err := d.install.groupMember(serviceUser)
	var c doctorCheck
	switch {
	case err != nil:
		c = skipCheck(sectionInstall, "install.log_group", fmt.Sprintf("cannot read the groups of %s (%v)", serviceUser, err))
	case !exists:
		c = skipCheck(sectionInstall, "install.log_group", fmt.Sprintf("this host has no %s group", d.install.logGroup))
	case !member:
		c = warnCheck(sectionInstall, "install.log_group",
			fmt.Sprintf("%s is not in the %s group, so %s cannot read /var/log", serviceUser, d.install.logGroup, strings.Join(probes, ", ")),
			fmt.Sprintf("sudo usermod -aG %s %s && sudo senhub-agent restart", d.install.logGroup, serviceUser))
	default:
		c = okCheck(sectionInstall, "install.log_group",
			fmt.Sprintf("%s is in the %s group (%s read /var/log)", serviceUser, d.install.logGroup, strings.Join(probes, ", ")))
	}
	return &c
}

func logDirCheck(d doctorDeps, serviceUser string) doctorCheck {
	if d.install.logDirAccess == nil {
		return skipCheck(sectionInstall, "install.log_dir", "the log directory is not checked on this platform")
	}
	state, err := d.install.logDirAccess(d.logDir, serviceUser)
	switch {
	case err != nil:
		return skipCheck(sectionInstall, "install.log_dir", fmt.Sprintf("cannot inspect %s (%v)", d.logDir, err))
	case !state.Exists:
		return warnCheck(sectionInstall, "install.log_dir",
			fmt.Sprintf("%s does not exist; the agent has not logged here yet", d.logDir), "senhub-agent start")
	case !state.Writable:
		return failCheck(sectionInstall, "install.log_dir",
			fmt.Sprintf("%s is not writable by the service (%s)", d.logDir, state.Detail),
			fmt.Sprintf("sudo chown -R %s %s", ownerOrDefault(serviceUser), d.logDir))
	default:
		return okCheck(sectionInstall, "install.log_dir", fmt.Sprintf("%s is writable (%s)", d.logDir, state.Detail))
	}
}

func ownerOrDefault(user string) string {
	if user == "" {
		return defaultServiceUser
	}
	return user
}

func checkConfiguration(d doctorDeps, cfg doctorConfig, st statusResult) []doctorCheck {
	out := configCheckChecks(d)
	out = append(out, secretsCheck(d))
	if c := licenseCheck(d, cfg); c != nil {
		out = append(out, *c)
	}
	out = append(out, httpPortCheck(d, cfg, st))
	return out
}

func configCheckChecks(d doctorDeps) []doctorCheck {
	fix := fmt.Sprintf("%s --config-path %s", doctorConfigHint, d.configPath)
	text, outcome, err := d.configCheck(d.configPath)
	if err != nil {
		return []doctorCheck{failCheck(sectionConfiguration, "config.check", fmt.Sprintf("the configuration check did not run (%v)", err), fix)}
	}
	if outcome.loadErr != nil {
		return []doctorCheck{failCheck(sectionConfiguration, "config.check", outcome.loadErr.Error(),
			"create the file with 'senhub-agent install', or pass --config-path with its location")}
	}
	var out []doctorCheck
	errorsFound := 0
	for _, f := range parseCheckFindings(text) {
		switch f.Level {
		case "error":
			errorsFound++
			out = append(out, failCheck(sectionConfiguration, "config.check", f.Message, fix))
		case "warn":
			out = append(out, warnCheck(sectionConfiguration, "config.check", f.Message, fix))
		}
	}
	if outcome.errors > 0 && errorsFound == 0 {
		out = append(out, failCheck(sectionConfiguration, "config.check", fmt.Sprintf("%d configuration error(s)", outcome.errors), fix))
	}
	if len(out) == 0 {
		out = append(out, okCheck(sectionConfiguration, "config.check", "the configuration is valid"))
	}
	return out
}

func secretsCheck(d doctorDeps) doctorCheck {
	rep, err := d.secretStore(filepath.Dir(d.configPath))
	switch {
	case err != nil:
		return failCheck(sectionConfiguration, "config.secrets", fmt.Sprintf("the secret store is not readable (%v)", err), "senhub-agent secret status")
	case !rep.Present:
		return okCheck(sectionConfiguration, "config.secrets", "no secret store in use")
	default:
		return okCheck(sectionConfiguration, "config.secrets", fmt.Sprintf("backend %s, %d secret(s)", rep.Backend, rep.Secrets))
	}
}

// licenseCheck covers the expiry only: whether the token is valid and
// bound to this agent is already a finding of config.check.
func licenseCheck(d doctorDeps, cfg doctorConfig) *doctorCheck {
	if cfg.err != nil {
		return nil
	}
	token := cfg.data.Agent.License
	if token == "" {
		c := okCheck(sectionConfiguration, "config.license", "no licence set (free tier)")
		return &c
	}
	if d.licenseExpiry == nil {
		return nil
	}
	tier, expires, err := d.licenseExpiry(token)
	if err != nil {
		return nil
	}
	days := int(expires.Sub(d.now()).Hours() / 24)
	var c doctorCheck
	switch {
	case expires.Before(d.now()):
		c = failCheck(sectionConfiguration, "config.license",
			fmt.Sprintf("the %s licence expired on %s", tier, expires.Format("2006-01-02")), "renew the licence, then 'senhub-agent license activate'")
	case days < licenseWarnDays:
		c = warnCheck(sectionConfiguration, "config.license",
			fmt.Sprintf("the %s licence expires on %s (%d days)", tier, expires.Format("2006-01-02"), days), "renew the licence, then 'senhub-agent license activate'")
	default:
		c = okCheck(sectionConfiguration, "config.license",
			fmt.Sprintf("%s licence valid until %s (%d days)", tier, expires.Format("2006-01-02"), days))
	}
	return &c
}

func httpPortCheck(d doctorDeps, cfg doctorConfig, st statusResult) doctorCheck {
	if cfg.err != nil {
		return skipCheck(sectionConfiguration, "config.http_port", "the configuration cannot be loaded")
	}
	hasHTTP := false
	for _, s := range cfg.data.Storage {
		if s.Name == "http" {
			hasHTTP = true
		}
	}
	if !hasHTTP {
		return skipCheck(sectionConfiguration, "config.http_port", "no http output is configured")
	}
	_, bind, port := d.httpListen(d.configPath)
	if daemonAnswered(st) {
		return okCheck(sectionConfiguration, "config.http_port", fmt.Sprintf("port %d is served by the running agent", port))
	}
	if bind == "" {
		bind = defaultHTTPBindAddress
	}
	if err := d.portFree(bind, port); err != nil {
		return failCheck(sectionConfiguration, "config.http_port", err.Error(),
			"stop the process holding the port, or set another one in strategies.d/00-http.yaml")
	}
	return okCheck(sectionConfiguration, "config.http_port", fmt.Sprintf("port %d is free on %s", port, bind))
}

func checkOutputs(d doctorDeps, cfg doctorConfig, st statusResult) []doctorCheck {
	if cfg.err != nil {
		return []doctorCheck{skipCheck(sectionOutputs, "outputs.test", "the configuration cannot be loaded")}
	}
	var out []doctorCheck
	if len(cfg.data.Storage) == 0 {
		return []doctorCheck{skipCheck(sectionOutputs, "outputs.test", "no output is configured")}
	}
	for _, s := range cfg.data.Storage {
		id := "outputs." + s.Name
		if s.Name == "http" {
			out = append(out, skipCheck(sectionOutputs, id, "served by this agent; see config.http_port"))
			continue
		}
		params := s.Params
		if params == nil {
			params = map[string]interface{}{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), doctorOutputTimeout)
		steps := d.outputTest(ctx, s.Name, params, cfg.data.Agent.Key, 0)
		cancel()
		out = append(out, outputResult(id, s.Name, steps))
	}
	if daemonAnswered(st) {
		for _, f := range st.system.StrategyFailures {
			msg := fmt.Sprintf("%s is not running in the agent: %s", f.Strategy, f.Reason)
			if f.Detail != "" {
				msg += " (" + f.Detail + ")"
			}
			out = append(out, failCheck(sectionOutputs, "outputs.running", msg, doctorConfigHint+", then senhub-agent restart"))
		}
	}
	return out
}

func outputResult(id, name string, steps []otlp.ConnectionStep) doctorCheck {
	var passed []string
	for _, s := range steps {
		if !s.Passed {
			return failCheck(sectionOutputs, id, fmt.Sprintf("%s: step %q failed: %s", name, s.Name, s.Error),
				"check the output parameters in the configuration and the network path to the target")
		}
		passed = append(passed, s.Name)
	}
	return okCheck(sectionOutputs, id, fmt.Sprintf("%s: reachable (%s)", name, strings.Join(passed, ", ")))
}

func checkProbes(d doctorDeps, st statusResult) []doctorCheck {
	if !daemonAnswered(st) {
		reason := "the running agent did not answer over HTTP"
		switch {
		case st.managerErr != nil && !errors.Is(st.managerErr, service.ErrNotInstalled):
			reason = "no service manager here and the agent did not answer over HTTP"
		case st.notRunning:
			reason = fmt.Sprintf("the service is %s", st.serviceState)
		case st.managerErr != nil:
			reason = "the service is not installed"
		}
		return []doctorCheck{skipCheck(sectionProbes, "probes.status", "not checkable: "+reason)}
	}
	probes := st.system.Probes
	if len(probes) == 0 && st.system.ProbesError != "" {
		return []doctorCheck{warnCheck(sectionProbes, "probes.status", "the probe list could not be read from the agent: "+st.system.ProbesError, doctorConfigHint)}
	}
	if len(probes) == 0 {
		return []doctorCheck{warnCheck(sectionProbes, "probes.status", "the agent reports no probe", doctorConfigHint)}
	}
	var out []doctorCheck
	for _, p := range probes {
		out = append(out, probeResult(d, p))
	}
	return out
}

func probeResult(d doctorDeps, p status.ProbeStatus) doctorCheck {
	id := "probes." + p.Name
	fix := fmt.Sprintf("senhub-agent run --filter probe.%s (foreground, with the probe's log)", p.Name)
	switch {
	case p.Status == "error" || p.LastError != "":
		msg := "last cycle failed"
		if p.LastError != "" {
			msg += ": " + p.LastError
		}
		if p.Status == "error" {
			return failCheck(sectionProbes, id, msg, fix)
		}
		return warnCheck(sectionProbes, id, msg, fix)
	case p.MetricsCount == 0 || p.LastUpdate.IsZero():
		return warnCheck(sectionProbes, id, "enabled but no data collected yet", fix)
	default:
		return okCheck(sectionProbes, id, fmt.Sprintf("%d metrics, last cycle %s", p.MetricsCount, formatAge(d.now().Sub(p.LastUpdate))))
	}
}

func checkHost(d doctorDeps, st statusResult) []doctorCheck {
	var out []doctorCheck
	out = append(out, diskCheck(d))
	if daemonAnswered(st) && st.system.Performance.Measured {
		perf := st.system.Performance
		out = append(out, okCheck(sectionHost, "host.memory", fmt.Sprintf("the agent uses %.0f MB (heap %.0f MB)", perf.MemoryUsageMB, perf.HeapMB)))
	} else {
		out = append(out, skipCheck(sectionHost, "host.memory", "the agent's memory is read from the running agent, which did not answer"))
	}
	return out
}

func diskCheck(d doctorDeps) doctorCheck {
	path := d.logDir
	free, err := d.freePercent(path)
	if err != nil {
		path = filepath.Dir(d.configPath)
		free, err = d.freePercent(path)
	}
	if err != nil {
		return skipCheck(sectionHost, "host.disk", fmt.Sprintf("cannot read the free space (%v)", err))
	}
	msg := fmt.Sprintf("%.1f%% free on the volume of %s", free, path)
	fix := "free space on that volume (old logs, backups), or move the log directory"
	switch {
	case free < diskFailPercent:
		return failCheck(sectionHost, "host.disk", msg, fix)
	case free < diskWarnPercent:
		return warnCheck(sectionHost, "host.disk", msg, fix)
	default:
		return okCheck(sectionHost, "host.disk", msg)
	}
}

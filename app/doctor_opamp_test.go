package app

import (
	"errors"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/status"
)

func TestDoctorSaysWhetherOpAMPIsConnected(t *testing.T) {
	on := doctorConfig{data: configuration.LocalConfigurationData{OpAMP: &configuration.OpAMPConfig{Enabled: true}}}
	daemon := func(op *status.OpAMPStatus) statusResult {
		return statusResult{source: "daemon", system: status.SystemStatus{OpAMP: op}}
	}
	cases := []struct {
		name  string
		cfg   doctorConfig
		st    statusResult
		level string
		msg   string
	}{
		{"unloadable configuration", doctorConfig{err: errors.New("x")}, daemon(nil), levelSkip, "cannot be loaded"},
		{"block absent", doctorConfig{}, daemon(nil), levelOK, "OpAMP is disabled"},
		{"block disabled", doctorConfig{data: configuration.LocalConfigurationData{OpAMP: &configuration.OpAMPConfig{}}}, daemon(nil), levelOK, "OpAMP is disabled"},
		{"agent not answering", on, statusResult{notRunning: true}, levelSkip, "did not report"},
		{"agent without the state", on, daemon(nil), levelSkip, "did not report"},
		{"connected", on, daemon(&status.OpAMPStatus{Enabled: true, Connected: true}), levelOK, "enabled and connected"},
		{"not connected", on, daemon(&status.OpAMPStatus{Enabled: true}), levelWarn, "not connected"},
	}
	for _, c := range cases {
		got := opampCheck(c.cfg, c.st)
		if got.ID != "config.opamp" || got.Level != c.level || !strings.Contains(got.Message, c.msg) {
			t.Errorf("%s: got %+v, want level %s with %q", c.name, got, c.level, c.msg)
		}
	}
}

package app

import (
	"testing"

	_ "senhub-agent.go/internal/agent/probes/syslog"
)

// The syslog probe takes port, protocol and bind_address, all with
// defaults. config check demanded a listen_address the probe has never
// read, and reported an error on a configuration that runs.
func TestConfigCheckAcceptsTheSyslogParamsTheProbeReads(t *testing.T) {
	errs, _ := validateProbeParams("syslog", "syslog", map[string]interface{}{
		"port": 1514, "protocol": "udp", "bind_address": "0.0.0.0",
	})
	if errs != 0 {
		t.Errorf("config check reported %d error(s) on a working syslog probe", errs)
	}
}

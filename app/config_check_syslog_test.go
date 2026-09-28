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

// ping_gateway takes no parameter (it pings the default route) and
// netscaler accepts an api_key instead of a password: the hand-written
// table config check kept beside the probe schemas demanded
// "destination" and "password", and failed both working configurations.
func TestConfigCheckTrustsTheSchemaForRequiredParams(t *testing.T) {
	if errs, _ := validateProbeParams("gw", "ping_gateway", map[string]interface{}{}); errs != 0 {
		t.Errorf("a ping_gateway with no parameter reported %d error(s)", errs)
	}
}

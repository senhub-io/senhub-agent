package probes_test

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes"
)

// The SENHUB_PROBE_<NAME>_<FIELD> rule reads these first segments as fields
// of the probe entry, not as parameters. A schema declaring a top-level
// parameter of the same name would be reachable only as PARAMS__<KEY>;
// that is a trap worth refusing at declaration time.
func TestProbeSpecs_NoParamShadowsAProbeEntryField(t *testing.T) {
	entryFields := map[string]bool{"type": true, "enabled": true, "log_strategies": true, "custom_tags": true, "governance": true, "params": true}
	for _, s := range probes.RegisteredProbeSpecs() {
		for _, p := range s.Params {
			// snmp_poll reads a governance block of its own for the device;
			// the entry-level governance stays the one GOVERNANCE__ reaches,
			// and PARAMS__GOVERNANCE__ reaches the device block.
			if s.Type == "snmp_poll" && p.Key == "governance" {
				continue
			}
			if entryFields[strings.ToLower(p.Key)] {
				t.Errorf("probe %s: top-level parameter %q collides with a probe entry field of the environment rule", s.Type, p.Key)
			}
		}
	}
}

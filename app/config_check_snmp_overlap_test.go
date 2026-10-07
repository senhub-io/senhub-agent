package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCheckWarnsWhenTwoSNMPPollProbesShareATarget(t *testing.T) {
	probes := "probes:\n" +
		"  - name: core-a\n    type: snmp_poll\n    params:\n      target: 10.0.0.1\n      mibs: [mib-2]\n" +
		"  - name: core-b\n    type: snmp_poll\n    params:\n      target: 10.0.0.1\n      port: 161\n      mibs: [mib-2]\n" +
		"  - name: core-c\n    type: snmp_poll\n    params:\n      target: 10.0.0.1\n      community: other\n      mibs: [mib-2]\n"
	path := filepath.Join(t.TempDir(), "agent-config.yaml")
	if err := os.WriteFile(path, []byte(parityConfig(parityStorage, probes)), 0o600); err != nil {
		t.Fatal(err)
	}

	var outcome checkOutcome
	out := captureStdout(t, func() { outcome = checkConfig(path) })

	if !strings.Contains(out, `[WARN] 10.0.0.1:161 is polled by snmp_poll probes "core-a" and "core-b"`) {
		t.Errorf("no overlap warning naming both probes:\n%s", out)
	}
	if strings.Contains(out, `"core-c" and`) || strings.Contains(out, `and "core-c"`) {
		t.Errorf("a probe with another community was reported as overlapping:\n%s", out)
	}
	if outcome.errors != 0 {
		t.Errorf("an overlap is a warning, got %d error(s):\n%s", outcome.errors, out)
	}
	if outcome.warnings == 0 {
		t.Errorf("the overlap was not counted as a warning:\n%s", out)
	}
}

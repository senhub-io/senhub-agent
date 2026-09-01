package app

import (
	"strings"
	"testing"
)

// TestConfigCheckRefusesAValueTheProbeWouldRefuse is the acceptance
// condition of #848: `agent config check` used to verify structure and
// never values, so `priority: 99` was reported [OK] and the probe then
// refused to start. An operator who runs the check before restarting a
// production agent was told the file was good.
func TestConfigCheckRefusesAValueTheProbeWouldRefuse(t *testing.T) {
	var errs int
	out := captureStdout(t, func() {
		errs, _ = validateProbeParams("journal", "linux_logs", map[string]interface{}{
			"priority": 99,
		})
	})

	if errs != 1 {
		t.Errorf("errors = %d, want 1 — a value the probe rejects must fail the check", errs)
	}
	if !strings.Contains(out, "priority") {
		t.Errorf("the report does not name the parameter:\n%s", out)
	}
}

// TestConfigCheckReportsAValueThatWasSilentlyDiscarded is #847 seen from
// the check: the probe starts, so this is a warning, but the operator
// must learn that the value they wrote is not the one in use.
func TestConfigCheckReportsAValueThatWasSilentlyDiscarded(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("journal", "linux_logs", map[string]interface{}{
			"priority": "pas-un-nombre",
		})
	})

	if errs != 0 {
		t.Errorf("errors = %d, want 0 — the probe starts, on its default", errs)
	}
	if warns != 1 {
		t.Errorf("warnings = %d, want 1", warns)
	}
	if !strings.Contains(out, "priority") || !strings.Contains(out, "pas-un-nombre") {
		t.Errorf("the report must name the parameter and the value written:\n%s", out)
	}
}

// TestConfigCheckAcceptsAValidProbe keeps the check quiet when there is
// nothing to say — a check that cries wolf on working files is worse
// than one that says nothing.
func TestConfigCheckAcceptsAValidProbe(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("journal", "linux_logs", map[string]interface{}{
			"priority": 6,
			"units":    []interface{}{"ssh.service"},
		})
	})

	if errs != 0 || warns != 0 {
		t.Errorf("errors = %d, warnings = %d, want 0/0 for a valid probe:\n%s", errs, warns, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a valid probe must print nothing, got:\n%s", out)
	}
}

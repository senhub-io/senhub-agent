package app

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with stdout redirected and returns what it
// printed. checkConfig reports to the operator by printing, so the text
// is the contract here.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = saved }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return string(out)
}

// TestConfigCheckReportsARenamedParameter: the configuration works, so
// this is a warning — and it has to carry the current spelling, since
// "that name is old" without the new one leaves the operator to guess.
func TestConfigCheckReportsARenamedParameter(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("db", "mysql", map[string]interface{}{
			"host":                "db.example.com",
			"expose_per_database": true,
		})
	})

	if errs != 0 {
		t.Errorf("errors = %d, want 0 — the old name still works", errs)
	}
	if warns != 1 {
		t.Errorf("warnings = %d, want 1", warns)
	}
	if !strings.Contains(out, "expose_per_database") || !strings.Contains(out, "per_database") {
		t.Errorf("the report does not name both spellings:\n%s", out)
	}
}

// TestConfigCheckFailsOnAParameterWithNoEffect is the acceptance
// condition of #842: every old name either works, or fails the check.
// Silence is what let `sslmode: require` mean a plaintext connection.
func TestConfigCheckFailsOnAParameterWithNoEffect(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("pg", "postgresql", map[string]interface{}{
			"host":        "db.example.com",
			"username":    "monitor",
			"password":    "secret",
			"bloat_top_n": 10,
		})
	})

	if errs != 1 {
		t.Errorf("errors = %d, want 1 — a parameter with no effect must fail the check", errs)
	}
	if warns != 0 {
		t.Errorf("warnings = %d, want 0", warns)
	}
	if !strings.Contains(out, "bloat_top_n") {
		t.Errorf("the report does not name the parameter:\n%s", out)
	}
	if !strings.Contains(out, "bloat") {
		t.Errorf("the report does not say why:\n%s", out)
	}
}

// TestConfigCheckReportsEveryLegacyParameter: a half-migrated file gets
// one line per parameter, not the first one and a stop.
func TestConfigCheckReportsEveryLegacyParameter(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("pg", "postgresql", map[string]interface{}{
			"host":                "db.example.com",
			"username":            "monitor",
			"password":            "secret",
			"database":            "appdb",   // read as written — not a legacy name
			"sslmode":             "require", // idem
			"expose_top_tables":   5,
			"bloat_top_n":         10,
			"expose_per_database": true,
		})
	})

	if errs != 3 {
		t.Errorf("errors = %d, want 3", errs)
	}
	if warns != 0 {
		t.Errorf("warnings = %d, want 0 — the connection parameters are read as written", warns)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != 3 {
		t.Errorf("printed %d lines for 3 parameters with no effect:\n%s", lines, out)
	}
	for _, quiet := range []string{"database", "sslmode"} {
		if strings.Contains(out, `param "`+quiet+`"`) {
			t.Errorf("reported %q, which this probe reads as written:\n%s", quiet, out)
		}
	}
}

// TestAProbeWithNoLegacyNamesIsUntouched keeps the check silent for the
// probes it has nothing to say about.
func TestAProbeWithNoLegacyNamesIsUntouched(t *testing.T) {
	var errs, warns int
	out := captureStdout(t, func() {
		errs, warns = validateProbeParams("cpu-probe", "cpu", map[string]interface{}{"interval": 30})
	})
	if errs != 0 || warns != 0 || out != "" {
		t.Errorf("cpu reported errs=%d warns=%d out=%q", errs, warns, out)
	}
}

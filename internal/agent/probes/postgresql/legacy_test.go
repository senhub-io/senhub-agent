package postgresql

import (
	"strings"
	"testing"
	"time"
)

// The postgresql probe took over the type from a paid probe (#476) whose
// parameters described the same connection in libpq's vocabulary. Those
// names are read again here — and `ca_cert` finally reaches the driver,
// which is the defect that made the rest worth doing: it was parsed,
// stored, and never sent, so a private CA was configured and never used.

func TestConnectionNamesAreReadAgain(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":                        "db.example.com",
		"username":                    "monitor",
		"password":                    "secret",
		"database":                    "appdb",
		"sslmode":                     "verify-ca",
		"sslrootcert":                 "/etc/ssl/db-ca.pem",
		"timeout":                     45,
		"max_replication_lag_seconds": 90,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Databases) != 1 || cfg.Databases[0] != "appdb" {
		t.Errorf("databases = %v, want [appdb] — `database` selects the connection's database", cfg.Databases)
	}
	if cfg.SSLMode != "verify-ca" {
		t.Errorf("sslmode = %q, want verify-ca", cfg.SSLMode)
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.CACert != "/etc/ssl/db-ca.pem" {
		t.Errorf("sslrootcert did not reach the TLS config: %+v", cfg.TLSConfig)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("timeout = %v, want 45s", cfg.Timeout)
	}
	if cfg.MaxReplicationLag != 90*time.Second {
		t.Errorf("max_replication_lag_seconds = %v, want 90s", cfg.MaxReplicationLag)
	}
}

// TestUnknownSSLModeIsRefused: passing it through would surface at
// connect time as a database that is down, which points the operator at
// the server rather than at their file.
func TestUnknownSSLModeIsRefused(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"sslmode": "verify_full", // underscore: a plausible typo
	})
	if err == nil {
		t.Fatal("an unknown sslmode was accepted")
	}
	if !strings.Contains(err.Error(), "verify_full") {
		t.Errorf("error %q does not name the offending value", err)
	}
}

// TestTLSBlockIsReadInBothShapes covers the decode that made the block
// inert: yaml.v2 gives nested maps interface keys, and the parser only
// matched the string-keyed shape.
func TestTLSBlockIsReadInBothShapes(t *testing.T) {
	for name, block := range map[string]interface{}{
		"string keys":    map[string]interface{}{"ca_cert": "/ca.pem", "insecure_skip_verify": true},
		"interface keys": map[interface{}]interface{}{"ca_cert": "/ca.pem", "insecure_skip_verify": true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := parseConfig(map[string]interface{}{
				"host": "db", "username": "u", "password": "p", "tls": block,
			})
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if cfg.TLSConfig == nil {
				t.Fatal("tls block was ignored")
			}
			if cfg.TLSConfig.CACert != "/ca.pem" || !cfg.TLSConfig.InsecureSkipVerify {
				t.Errorf("tls block read as %+v", cfg.TLSConfig)
			}
		})
	}
}

func TestTLSAcceptsTheOtherSpellings(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"tls": map[string]interface{}{"ca_file": "/ca.pem", "skip_verify": true},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.CACert != "/ca.pem" || !cfg.TLSConfig.InsecureSkipVerify {
		t.Errorf("ca_file / skip_verify were not read: %+v", cfg.TLSConfig)
	}
}

// ── the DSN ──────────────────────────────────────────────────────────

func dsnFor(t *testing.T, params map[string]interface{}) string {
	t.Helper()
	cfg, err := parseConfig(params)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	return (&pgProbe{cfg: cfg}).buildDSN()
}

// TestDSNCarriesTheConfiguredCA is the regression. The CA was read out
// of the configuration and dropped on the floor: verification fell back
// to the system roots, so a certificate signed by a private authority
// failed for a reason no message explained.
func TestDSNCarriesTheConfiguredCA(t *testing.T) {
	dsn := dsnFor(t, map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"tls": map[string]interface{}{"ca_cert": "/etc/ssl/db-ca.pem"},
	})
	if !strings.Contains(dsn, "sslrootcert=") {
		t.Errorf("DSN %q carries no sslrootcert — the configured CA never reaches the driver", dsn)
	}
	if !strings.Contains(dsn, "sslmode=verify-full") {
		t.Errorf("DSN %q should verify when a CA is configured", dsn)
	}
}

// TestExplicitSSLModeWins: an operator writing libpq's own vocabulary
// gets exactly that, including the modes the tls block cannot express.
func TestExplicitSSLModeWins(t *testing.T) {
	dsn := dsnFor(t, map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"sslmode": "require",
		"tls":     map[string]interface{}{"ca_cert": "/ca.pem"},
	})
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("DSN %q did not honour the explicit sslmode", dsn)
	}
}

// TestDefaultsAreUnchanged guards the upgrade: a configuration that says
// nothing about TLS or lag must behave exactly as it did before this
// change, or every existing install shifts under its operators.
func TestDefaultsAreUnchanged(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MaxReplicationLag != 300*time.Second {
		t.Errorf("default lag threshold = %v, want the 300s the probe has always applied", cfg.MaxReplicationLag)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want the 10s the probe has always applied", cfg.Timeout)
	}
	if dsn := (&pgProbe{cfg: cfg}).buildDSN(); !strings.Contains(dsn, "sslmode=prefer") {
		t.Errorf("DSN %q changed the default TLS posture", dsn)
	}
}

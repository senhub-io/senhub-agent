package mysql

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The mysql probe took over the type from a paid probe with a different
// parameter set (#476), and for a cycle it answered to neither the names
// the documentation gave nor a warning. These pin the names it answers
// to now — old and new — and the capabilities the old set had that the
// new one had lost.

func TestLegacyNamesStillConfigureTheProbe(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":                "db.example.com",
		"username":            "monitor",
		"password":            "secret",
		"expose_per_database": true,
		"expose_top_tables":   25,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if !cfg.PerDatabase {
		t.Error("expose_per_database did not turn on per-database metrics")
	}
	// One key became two: asking for 25 tables implied asking for tables.
	if !cfg.PerTable {
		t.Error("expose_top_tables did not turn on per-table metrics")
	}
	if cfg.TopNTables != 25 {
		t.Errorf("top_n_tables = %d, want 25", cfg.TopNTables)
	}
}

// TestCurrentNamesWinOverLegacy: a configuration migrated halfway must
// land on what the operator wrote most recently, not on parser order.
func TestCurrentNamesWinOverLegacy(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":                "db",
		"username":            "u",
		"password":            "p",
		"expose_per_database": true,
		"per_database":        false,
		"expose_top_tables":   25,
		"top_n_tables":        5,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.PerDatabase {
		t.Error("per_database: false was overridden by the legacy spelling")
	}
	if cfg.TopNTables != 5 {
		t.Errorf("top_n_tables = %d, want 5 — the current spelling must win", cfg.TopNTables)
	}
}

// ── TLS ──────────────────────────────────────────────────────────────

// writeTestCA writes a self-signed certificate, so the positive path
// exercises a real PEM parse rather than a string that happens not to be
// empty.
func writeTestCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "senhub-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing CA: %v", err)
	}
	return path
}

func TestTLSAcceptsBothShapes(t *testing.T) {
	ca := writeTestCA(t)

	cases := []struct {
		name       string
		raw        interface{}
		enabled    bool
		skipVerify bool
		caFile     string
	}{
		{"bool true", true, true, false, ""},
		{"bool false", false, false, false, ""},
		{"absent", nil, false, false, ""},
		{
			name:    "block with a CA",
			raw:     map[string]interface{}{"enabled": true, "ca_file": ca},
			enabled: true, caFile: ca,
		},
		{
			// Naming a CA and leaving TLS off is never what someone meant.
			name:    "a CA implies TLS",
			raw:     map[string]interface{}{"ca_file": ca},
			enabled: true, caFile: ca,
		},
		{
			name:    "skip_verify implies TLS",
			raw:     map[string]interface{}{"skip_verify": true},
			enabled: true, skipVerify: true,
		},
		{
			// The spelling the OTLP strategy and the postgresql probe use.
			name:    "insecure_skip_verify is the same option",
			raw:     map[string]interface{}{"insecure_skip_verify": true},
			enabled: true, skipVerify: true,
		},
		{
			// yaml.v2 hands nested blocks over with interface keys. The
			// postgresql probe read only the other shape, which is how its
			// tls block went unread.
			name:    "interface-keyed block",
			raw:     map[interface{}]interface{}{"ca_file": ca},
			enabled: true, caFile: ca,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseTLS(c.raw)
			if err != nil {
				t.Fatalf("parseTLS: %v", err)
			}
			if got.Enabled != c.enabled {
				t.Errorf("enabled = %v, want %v", got.Enabled, c.enabled)
			}
			if got.SkipVerify != c.skipVerify {
				t.Errorf("skip_verify = %v, want %v", got.SkipVerify, c.skipVerify)
			}
			if got.CAFile != c.caFile {
				t.Errorf("ca_file = %q, want %q", got.CAFile, c.caFile)
			}
		})
	}
}

// TestDSNCarriesEveryConnectionParameter is where the restored options
// have to show up, because everything above them is bookkeeping until
// the driver sees it.
func TestDSNCarriesEveryConnectionParameter(t *testing.T) {
	ca := writeTestCA(t)

	p := newProbeForTest(t, map[string]interface{}{
		"host":     "db.example.com",
		"port":     3307,
		"username": "monitor",
		"password": "secret",
		"database": "appdb",
		"timeout":  "25s",
		"tls":      map[string]interface{}{"ca_file": ca},
	})

	dsn, err := p.buildDSN()
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}

	for _, want := range []string{
		"appdb",       // the database the connection opens on
		"timeout=25s", // dial
		"readTimeout=25s",
		"writeTimeout=25s",
		"tls=senhub-mysql-", // a registered config, not the built-in "true"
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN %q does not carry %q", dsn, want)
		}
	}
}

// TestTLSWithoutCAUsesTheDriverBuiltin keeps the common case free of a
// registration nobody needs.
func TestTLSWithoutCAUsesTheDriverBuiltin(t *testing.T) {
	p := newProbeForTest(t, map[string]interface{}{
		"host": "db", "username": "u", "password": "p", "tls": true,
	})
	dsn, err := p.buildDSN()
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN %q should select the driver's built-in verification", dsn)
	}
}

// TestUnusableCAIsAnError is the half that matters most: falling back to
// the system roots would verify against the wrong authority and look
// like it worked.
func TestUnusableCAIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.pem")
	p := newProbeForTest(t, map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"tls": map[string]interface{}{"ca_file": missing},
	})
	if _, err := p.buildDSN(); err == nil {
		t.Fatal("a missing CA file produced a DSN — the connection would verify against the system roots instead")
	}

	garbage := filepath.Join(t.TempDir(), "garbage.pem")
	if err := os.WriteFile(garbage, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	p = newProbeForTest(t, map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"tls": map[string]interface{}{"ca_file": garbage},
	})
	if _, err := p.buildDSN(); err == nil {
		t.Fatal("a CA file with no certificate in it produced a DSN")
	}
}

func newProbeForTest(t *testing.T, params map[string]interface{}) *mysqlProbe {
	t.Helper()
	probe, err := NewMysqlProbe(params, nil)
	if err != nil {
		t.Fatalf("NewMysqlProbe: %v", err)
	}
	p, ok := probe.(*mysqlProbe)
	if !ok {
		t.Fatalf("constructor returned %T", probe)
	}
	p.SetName("test")
	return p
}

// ── replication health ───────────────────────────────────────────────

// TestReplicaHealthCountsLag is the behaviour change. A replica whose
// threads are both running but which is hours behind used to report
// healthy: the metric said the replication was fine while the data was
// not, which is the failure it exists to catch.
func TestReplicaHealthCountsLag(t *testing.T) {
	const maxLag = 300 * time.Second

	cases := []struct {
		name        string
		ioOK, sqlOK float64
		lag         float64
		maxLag      time.Duration
		wantHealthy bool
	}{
		{"threads up, no lag", 1, 1, 0, maxLag, true},
		{"threads up, lag under the threshold", 1, 1, 120, maxLag, true},
		{"threads up, lag over the threshold", 1, 1, 900, maxLag, false},
		{"lag exactly at the threshold", 1, 1, 300, maxLag, true},
		{"io thread down", 0, 1, 0, maxLag, false},
		{"sql thread down", 1, 0, 0, maxLag, false},
		// A deliberately delayed replica: the operator turns the term off
		// rather than living with a permanently unhealthy series.
		{"threshold disabled", 1, 1, 86400, 0, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := replicaHealth(c.ioOK, c.sqlOK, c.lag, c.maxLag)
			want := float64(0)
			if c.wantHealthy {
				want = 1
			}
			if got != want {
				t.Errorf("replicaHealth(io=%v sql=%v lag=%vs max=%v) = %v, want %v",
					c.ioOK, c.sqlOK, c.lag, c.maxLag, got, want)
			}
		})
	}
}

func TestReplicaThreadStateReadsShowReplicaStatus(t *testing.T) {
	ioOK, sqlOK, lag := replicaThreadState(map[string]string{
		"Slave_IO_Running":      "Yes",
		"Slave_SQL_Running":     "No",
		"Seconds_Behind_Master": "42",
	})
	if ioOK != 1 || sqlOK != 0 || lag != 42 {
		t.Errorf("got io=%v sql=%v lag=%v, want 1 0 42", ioOK, sqlOK, lag)
	}
}

// TestMaxReplicationLagIsConfigurable pins the parameter itself: the
// threshold is the operator's, and the default is the value the
// postgresql probe has always used.
func TestMaxReplicationLagIsConfigurable(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MaxReplicationLag != 300*time.Second {
		t.Errorf("default max_replication_lag_seconds = %v, want 300s", cfg.MaxReplicationLag)
	}

	cfg, err = parseConfig(map[string]interface{}{
		"host": "db", "username": "u", "password": "p",
		"max_replication_lag_seconds": 60,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MaxReplicationLag != 60*time.Second {
		t.Errorf("max_replication_lag_seconds = %v, want 60s", cfg.MaxReplicationLag)
	}
}

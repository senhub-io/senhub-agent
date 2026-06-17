package mssql

import (
	"net/url"
	"strings"
	"testing"
)

// TestParseConfig_EncryptDefaultsToTrue pins the secure default: a config
// that does not mention encryption must still negotiate TLS.
func TestParseConfig_EncryptDefaultsToTrue(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"host": "db1"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Encrypt != "true" {
		t.Errorf("default encrypt = %q, want \"true\"", cfg.Encrypt)
	}
}

// TestParseConfig_EncryptAllowlist rejects values outside the go-mssqldb
// vocabulary so a typo cannot silently produce an unexpected wire mode.
func TestParseConfig_EncryptAllowlist(t *testing.T) {
	for _, v := range []string{"true", "false", "disable", "strict"} {
		if _, err := parseConfig(map[string]interface{}{"host": "db1", "encrypt": v}); err != nil {
			t.Errorf("encrypt=%q rejected unexpectedly: %v", v, err)
		}
	}
	if _, err := parseConfig(map[string]interface{}{"host": "db1", "encrypt": "yes"}); err == nil {
		t.Error("encrypt=\"yes\" accepted, want rejection")
	}
}

// TestBuildDSN_NoParameterSmuggling is the regression test for the DSN
// injection the ';'-joined form allowed: a password carrying ';encrypt=disable'
// must stay inside the password component, never downgrade the connection.
func TestBuildDSN_NoParameterSmuggling(t *testing.T) {
	cfg := config{
		Host:     "db1",
		Port:     1433,
		Username: "svc",
		Password: "p;encrypt=disable;TrustServerCertificate=true",
		Encrypt:  "true",
	}
	dsn := buildDSN(cfg)

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("buildDSN produced an unparseable URL %q: %v", dsn, err)
	}
	if got := u.Query().Get("encrypt"); got != "true" {
		t.Errorf("encrypt smuggled to %q via the password, want \"true\"", got)
	}
	if got := u.Query().Get("TrustServerCertificate"); got != "false" {
		t.Errorf("TrustServerCertificate smuggled to %q, want \"false\"", got)
	}
	pw, _ := u.User.Password()
	if pw != cfg.Password {
		t.Errorf("password round-trip = %q, want %q", pw, cfg.Password)
	}
	// In URL-mode DSNs the driver reads parameters only from the query
	// string; a ';' inside the userinfo is not a separator. The smuggled
	// "encrypt=disable" therefore lives inside the password component, never
	// as a connection parameter — which the re-parse above proves. (The
	// ';'-joined ADO form this replaced had no such boundary.)
	if !strings.Contains(u.RawQuery, "encrypt=true") {
		t.Errorf("query lost the secure encrypt setting: %s", u.RawQuery)
	}
}

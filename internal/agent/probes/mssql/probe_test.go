package mssql

import (
	"net/url"
	"strings"
	"testing"
)

// TestEntitySource_LocalDBRunsOnHost: the db.instance.id is host:port-derived
// ("mssql://<host>:<port>"), so it embeds the loopback literal and is identical
// on every host. The collapse guard therefore refuses the runs_on even on a
// loopback target (anchoring it would false-join hosts). A remote db never
// anchors either. So no runs_on edge is ever emitted here — the edge is wired
// for correctness but the gate suppresses it.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	hasRunsOn := func(host string) bool {
		src := newEntitySource(host, 1433)
		obs, _ := src.Observe()
		for _, r := range obs.Relations {
			if r.Type == "runs_on" {
				return true
			}
		}
		return false
	}
	if hasRunsOn("127.0.0.1") {
		t.Error("host:port id must NOT emit runs_on on loopback (collapse guard)")
	}
	if hasRunsOn("localhost") {
		t.Error("host:port id must NOT emit runs_on on localhost (collapse guard)")
	}
	if hasRunsOn("10.0.0.5") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

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

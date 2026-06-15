package postgresql

import (
	"net/url"
	"testing"
	"time"
)

// TestParseConfig_Defaults exercises the config parser with the minimum
// required fields and verifies that defaults are applied.
func TestParseConfig_Defaults(t *testing.T) {
	params := map[string]interface{}{
		"host":     "pg.example.com",
		"username": "mon",
		"password": "secret",
	}

	cfg, err := parseConfig(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Host != "pg.example.com" {
		t.Errorf("host: got %q, want %q", cfg.Host, "pg.example.com")
	}
	if cfg.Port != 5432 {
		t.Errorf("port: got %d, want 5432", cfg.Port)
	}
	if cfg.Username != "mon" {
		t.Errorf("username: got %q, want %q", cfg.Username, "mon")
	}
	if cfg.Password != "secret" {
		t.Errorf("password: got %q, want %q", cfg.Password, "secret")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("interval: got %v, want 60s", cfg.Interval)
	}
	if cfg.TLSConfig != nil {
		t.Errorf("TLSConfig: expected nil by default, got %v", cfg.TLSConfig)
	}
}

// TestParseConfig_AllFields exercises non-default values.
func TestParseConfig_AllFields(t *testing.T) {
	params := map[string]interface{}{
		"host":     "10.0.0.1",
		"port":     5433,
		"username": "admin",
		"password": "pass123",
		"databases": []interface{}{"app", "reporting"},
		"interval": 30,
		"tls": map[string]interface{}{
			"insecure_skip_verify": true,
		},
	}

	cfg, err := parseConfig(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 5433 {
		t.Errorf("port: got %d, want 5433", cfg.Port)
	}
	if len(cfg.Databases) != 2 {
		t.Errorf("databases: got %d, want 2", len(cfg.Databases))
	}
	if cfg.Databases[0] != "app" {
		t.Errorf("databases[0]: got %q, want %q", cfg.Databases[0], "app")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval: got %v, want 30s", cfg.Interval)
	}
	if cfg.TLSConfig == nil {
		t.Fatal("TLSConfig: expected non-nil")
	}
	if !cfg.TLSConfig.InsecureSkipVerify {
		t.Error("TLSConfig.InsecureSkipVerify: expected true")
	}
}

// TestParseConfig_MissingHost verifies that a missing host returns an error.
func TestParseConfig_MissingHost(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"username": "mon",
		"password": "pass",
	})
	if err == nil {
		t.Fatal("expected error for missing host, got nil")
	}
}

// TestParseConfig_MissingUsername verifies that a missing username returns an error.
func TestParseConfig_MissingUsername(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host":     "db.local",
		"password": "pass",
	})
	if err == nil {
		t.Fatal("expected error for missing username, got nil")
	}
}

// TestParseConfig_MissingPassword verifies that a missing password returns an error.
func TestParseConfig_MissingPassword(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host":     "db.local",
		"username": "mon",
	})
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
}

// TestBuildDSN_NoTLS verifies the URL DSN and the opportunistic default
// when TLS is not explicitly configured (sslmode=prefer, not disable).
func TestBuildDSN_NoTLS(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:     "pg.local",
			Port:     5432,
			Username: "mon",
			Password: "pw",
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/postgres?sslmode=prefer"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_WithDatabase verifies that the first configured database
// is used in the DSN path.
func TestBuildDSN_WithDatabase(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:      "pg.local",
			Port:      5432,
			Username:  "mon",
			Password:  "pw",
			Databases: []string{"mydb"},
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/mydb?sslmode=prefer"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_TLSInsecure verifies sslmode=require when insecure_skip_verify.
func TestBuildDSN_TLSInsecure(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:      "pg.local",
			Port:      5432,
			Username:  "mon",
			Password:  "pw",
			TLSConfig: &pgTLSConfig{InsecureSkipVerify: true},
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/postgres?sslmode=require"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_NoParameterSmuggling is the regression test for the keyword/
// value injection the space-joined form allowed: a password carrying
// " host=evil.com sslmode=disable" must stay inside the password component
// and never redirect the connection or downgrade TLS.
func TestBuildDSN_NoParameterSmuggling(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:     "pg.local",
			Port:     5432,
			Username: "mon",
			Password: "pw host=evil.com sslmode=disable",
		},
	}

	dsn := p.buildDSN()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("buildDSN produced an unparseable URL %q: %v", dsn, err)
	}
	if u.Hostname() != "pg.local" {
		t.Errorf("host redirected to %q via the password, want pg.local", u.Hostname())
	}
	if got := u.Query().Get("sslmode"); got != "prefer" {
		t.Errorf("sslmode smuggled to %q, want prefer", got)
	}
	pw, _ := u.User.Password()
	if pw != p.cfg.Password {
		t.Errorf("password round-trip = %q, want %q", pw, p.cfg.Password)
	}
}

// TestEntitySource_NotReadyBeforeFirstUpdate verifies ok=false
// before any update cycle has run.
func TestEntitySource_NotReadyBeforeFirstUpdate(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)

	_, ok := src.Observe()
	if ok {
		t.Error("Observe should return ok=false before the first update")
	}
}

// TestEntitySource_ReadyAfterUpdate verifies that one update cycle
// transitions ok to true and populates entity identity.
func TestEntitySource_ReadyAfterUpdate(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)

	src.update("pg.local:5432")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe should return ok=true after update")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}

	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity type: got %q, want %q", e.Type, "db")
	}
	if e.ID["db.system.name"] != "postgresql" {
		t.Errorf("db.system.name: got %v, want postgresql", e.ID["db.system.name"])
	}
	want := "postgresql://pg.local:5432"
	if e.ID["db.instance.id"] != want {
		t.Errorf("db.instance.id: got %v, want %s", e.ID["db.instance.id"], want)
	}
}

// TestProbeType verifies the stable identifier constant.
func TestProbeType(t *testing.T) {
	if ProbeType != "postgresql" {
		t.Errorf("ProbeType: got %q, want %q", ProbeType, "postgresql")
	}
}

package postgresql

import (
	"strings"
	"testing"
)

func TestParseConfig_MinimumRequired(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":     "db.example.com",
		"username": "monitor",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port default = %d, want 5432", cfg.Port)
	}
	if cfg.Database != "postgres" {
		t.Errorf("Database default = %q, want postgres", cfg.Database)
	}
	if cfg.SSLMode != "prefer" {
		t.Errorf("SSLMode default = %q, want prefer", cfg.SSLMode)
	}
	if cfg.BloatTopN != 10 {
		t.Errorf("BloatTopN default = %d, want 10", cfg.BloatTopN)
	}
}

func TestParseConfig_BloatCappedAt50(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":        "x",
		"username":    "u",
		"password":    "p",
		"bloat_top_n": 1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BloatTopN != 50 {
		t.Errorf("BloatTopN = %d, want 50 (hard cap)", cfg.BloatTopN)
	}
}

func TestParseConfig_RejectsMissingHost(t *testing.T) {
	if _, err := parseConfig(map[string]interface{}{"username": "u"}); err == nil {
		t.Errorf("missing host should error")
	}
}

func TestBuildDSN_BasicFields(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 5432,
		Username: "monitor", Password: "secret",
		Database: "production", SSLMode: "require", Timeout: 10,
	})
	want := []string{
		"host=db.example.com",
		"port=5432",
		"user=monitor",
		"password='secret'",
		"dbname=production",
		"sslmode=require",
		"connect_timeout=10",
	}
	for _, w := range want {
		if !strings.Contains(dsn, w) {
			t.Errorf("DSN %q missing %q", dsn, w)
		}
	}
}

func TestBuildDSN_EscapesPasswordSingleQuote(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "x", Port: 5432, Username: "u",
		Password: "it's-tricky", Database: "postgres",
		SSLMode: "disable", Timeout: 5,
	})
	if !strings.Contains(dsn, `password='it\'s-tricky'`) {
		t.Errorf("password escaping wrong: %q", dsn)
	}
}

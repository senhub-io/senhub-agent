package mysql

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
	if cfg.Host != "db.example.com" {
		t.Errorf("Host = %q, want db.example.com", cfg.Host)
	}
	if cfg.Port != 3306 {
		t.Errorf("Port default = %d, want 3306", cfg.Port)
	}
	if cfg.Interval != 60 {
		t.Errorf("Interval default = %d, want 60", cfg.Interval)
	}
	if cfg.Timeout != 10 {
		t.Errorf("Timeout default = %d, want 10", cfg.Timeout)
	}
	if cfg.MaxReplicationLagSeconds != 60 {
		t.Errorf("MaxReplicationLagSeconds default = %d, want 60", cfg.MaxReplicationLagSeconds)
	}
	if cfg.MaxHeartbeatAgeSeconds != 300 {
		t.Errorf("MaxHeartbeatAgeSeconds default = %d, want 300", cfg.MaxHeartbeatAgeSeconds)
	}
	if cfg.ExposePerDatabase {
		t.Errorf("ExposePerDatabase should default to false")
	}
	if cfg.ExposeTopTables != 0 {
		t.Errorf("ExposeTopTables default = %d, want 0", cfg.ExposeTopTables)
	}
}

func TestParseConfig_RejectsMissingHost(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"username": "monitor",
	})
	if err == nil {
		t.Errorf("missing host should error")
	}
}

func TestParseConfig_RejectsMissingUsername(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host": "db.example.com",
	})
	if err == nil {
		t.Errorf("missing username should error")
	}
}

func TestParseConfig_TLSBlock(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":     "db.example.com",
		"username": "monitor",
		"password": "secret",
		"tls": map[string]interface{}{
			"enabled":     true,
			"skip_verify": true,
			"ca_file":     "/etc/ssl/db-ca.pem",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.TLSEnabled || !cfg.TLSSkipVerify || cfg.TLSCAFile != "/etc/ssl/db-ca.pem" {
		t.Errorf("TLS block not fully parsed: %+v", cfg)
	}
}

func TestParseConfig_OptInFlags(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":                     "db.example.com",
		"username":                 "monitor",
		"password":                 "secret",
		"expose_per_database":      true,
		"include_system_databases": true,
		"expose_top_tables":        25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ExposePerDatabase {
		t.Errorf("ExposePerDatabase not picked up")
	}
	if !cfg.IncludeSystemDatabases {
		t.Errorf("IncludeSystemDatabases not picked up")
	}
	if cfg.ExposeTopTables != 25 {
		t.Errorf("ExposeTopTables = %d, want 25", cfg.ExposeTopTables)
	}
}

func TestBuildDSN_TLSDisabled(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Database: "production", Timeout: 10,
	})
	// The exact DSN format is driver-specific, but a few invariants
	// must hold or the connection will fail at runtime.
	want := []string{
		"monitor:secret",
		"@tcp(db.example.com:3306)",
		"/production",
		"timeout=10s",
		"tls=false",
		"parseTime=true",
	}
	for _, w := range want {
		if !strings.Contains(dsn, w) {
			t.Errorf("DSN %q missing %q", dsn, w)
		}
	}
}

func TestBuildDSN_TLSSkipVerify(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Timeout: 10, TLSEnabled: true, TLSSkipVerify: true,
	})
	if !strings.Contains(dsn, "tls=skip-verify") {
		t.Errorf("DSN should set tls=skip-verify, got %q", dsn)
	}
}

func TestBuildDSN_TLSEnabledVerify(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Timeout: 10, TLSEnabled: true, TLSSkipVerify: false,
	})
	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN should set tls=true, got %q", dsn)
	}
}

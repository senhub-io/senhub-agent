package postgresql

import (
	"fmt"
)

// probeConfig holds the validated configuration for one PostgreSQL
// probe instance. Mirrors the user-facing YAML keys 1:1 (see
// DESIGN §3).
type probeConfig struct {
	Host                    string
	Port                    int
	Username                string
	Password                string
	Database                string // libpq DB name; defaults to "postgres"
	Interval                int
	Timeout                 int
	SSLMode                 string // libpq sslmode: disable, allow, prefer, require, verify-ca, verify-full
	SSLRootCert             string
	ExposePerDatabase       bool
	IncludeSystemDatabases  bool
	ExposeTopTables         int
	BloatTopN               int

	MaxReplicationLagSeconds int
	MaxHeartbeatAgeSeconds   int
}

func parseConfig(config map[string]interface{}) (*probeConfig, error) {
	host, ok := config["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("postgresql probe requires 'host' configuration")
	}
	username, ok := config["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("postgresql probe requires 'username' configuration")
	}

	password, _ := config["password"].(string)

	port := 5432
	if v, ok := config["port"].(int); ok {
		port = v
	}
	interval := 60
	if v, ok := config["interval"].(int); ok && v > 0 {
		interval = v
	}
	timeout := 10
	if v, ok := config["timeout"].(int); ok && v > 0 {
		timeout = v
	}
	database := "postgres"
	if v, ok := config["database"].(string); ok && v != "" {
		database = v
	}
	// libpq sslmode values: disable | allow | prefer | require |
	// verify-ca | verify-full. Default 'prefer' matches what most
	// clients do today and works against managed DBs without
	// requiring a CA bundle.
	sslmode := "prefer"
	if v, ok := config["sslmode"].(string); ok && v != "" {
		sslmode = v
	}
	sslRootCert, _ := config["sslrootcert"].(string)

	cfg := &probeConfig{
		Host:                     host,
		Port:                     port,
		Username:                 username,
		Password:                 password,
		Database:                 database,
		Interval:                 interval,
		Timeout:                  timeout,
		SSLMode:                  sslmode,
		SSLRootCert:              sslRootCert,
		BloatTopN:                10,
		MaxReplicationLagSeconds: 60,
		MaxHeartbeatAgeSeconds:   300,
	}

	if v, ok := config["expose_per_database"].(bool); ok {
		cfg.ExposePerDatabase = v
	}
	if v, ok := config["include_system_databases"].(bool); ok {
		cfg.IncludeSystemDatabases = v
	}
	if v, ok := config["expose_top_tables"].(int); ok && v > 0 {
		cfg.ExposeTopTables = v
	}
	if v, ok := config["bloat_top_n"].(int); ok && v > 0 {
		// Hard cap at 50 — pgstattuple_approx is non-trivial and a
		// runaway N would dominate the per-cycle budget.
		if v > 50 {
			v = 50
		}
		cfg.BloatTopN = v
	}
	if v, ok := config["max_replication_lag_seconds"].(int); ok && v > 0 {
		cfg.MaxReplicationLagSeconds = v
	}
	if v, ok := config["max_heartbeat_age_seconds"].(int); ok && v > 0 {
		cfg.MaxHeartbeatAgeSeconds = v
	}

	return cfg, nil
}

package mysql

import (
	"fmt"
)

// probeConfig holds the validated configuration for one MySQL/MariaDB
// probe instance. Fields mirror the user-facing YAML keys 1:1 so the
// parse function is easy to audit against the user docs.
type probeConfig struct {
	Host                   string
	Port                   int
	Username               string
	Password               string
	Database               string
	Interval               int
	Timeout                int
	TLSEnabled             bool
	TLSSkipVerify          bool
	TLSCAFile              string
	ExposePerDatabase      bool
	IncludeSystemDatabases bool
	ExposeTopTables        int

	// Composite replication health thresholds — used to derive the
	// senhub.db.replication.health gauge (see DESIGN §5.2).
	MaxReplicationLagSeconds int
	MaxHeartbeatAgeSeconds   int
}

func parseConfig(config map[string]interface{}) (*probeConfig, error) {
	host, ok := config["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("mysql probe requires 'host' configuration")
	}

	username, ok := config["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("mysql probe requires 'username' configuration")
	}

	password, _ := config["password"].(string)
	// Empty password is technically valid for localhost UNIX socket
	// connections, so we don't reject it here. The driver will error
	// later if the server refuses.

	port := 3306
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

	database, _ := config["database"].(string)

	cfg := &probeConfig{
		Host:                     host,
		Port:                     port,
		Username:                 username,
		Password:                 password,
		Database:                 database,
		Interval:                 interval,
		Timeout:                  timeout,
		MaxReplicationLagSeconds: 60,
		MaxHeartbeatAgeSeconds:   300,
	}

	// TLS block is optional. The structure mirrors the http storage
	// TLS config to keep the user experience consistent across probes
	// and the agent's own HTTPS endpoint.
	if tlsMap, ok := config["tls"].(map[string]interface{}); ok {
		if v, ok := tlsMap["enabled"].(bool); ok {
			cfg.TLSEnabled = v
		}
		if v, ok := tlsMap["skip_verify"].(bool); ok {
			cfg.TLSSkipVerify = v
		}
		if v, ok := tlsMap["ca_file"].(string); ok {
			cfg.TLSCAFile = v
		}
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

	if v, ok := config["max_replication_lag_seconds"].(int); ok && v > 0 {
		cfg.MaxReplicationLagSeconds = v
	}
	if v, ok := config["max_heartbeat_age_seconds"].(int); ok && v > 0 {
		cfg.MaxHeartbeatAgeSeconds = v
	}

	return cfg, nil
}

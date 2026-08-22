package postgresql

import (
	"fmt"
	"senhub-agent.go/internal/agent/probes/types"
	"time"
)

// config holds the validated probe configuration.
type config struct {
	Host     string
	Port     int
	Username string
	Password string
	// Databases names the database the connection opens on. The probe
	// reads server-wide views (pg_stat_database and friends are already
	// cluster-wide), so only the first entry is used and it selects the
	// connection's database, nothing else.
	Databases []string
	Interval  time.Duration
	Timeout   time.Duration
	TLSConfig *pgTLSConfig // nil = no explicit TLS settings
	// SSLMode, when set, is passed to the driver verbatim. It is the
	// libpq vocabulary an operator already knows, and the only way to
	// express modes the tls block does not model (allow, verify-ca).
	SSLMode string
	// MaxReplicationLag is the replay lag past which a streaming replica
	// counts as unhealthy.
	MaxReplicationLag time.Duration
	InstanceName      string // optional stable override for db.instance.id (Toise identity)
}

// sslModes is the libpq set. An unknown value is refused rather than
// passed through: the driver would reject it at connect time, which
// surfaces as a down database rather than as a configuration error.
var sslModes = map[string]bool{
	"disable": true, "allow": true, "prefer": true,
	"require": true, "verify-ca": true, "verify-full": true,
}

// parseConfig converts the free-form params map from the probe YAML
// into a typed config. Required: host, username, password. Defaults:
// port=5432, interval=60s.
func parseConfig(params map[string]interface{}) (config, error) {
	cfg := config{
		Port:              5432,
		Interval:          defaultInterval,
		Timeout:           defaultTimeout,
		MaxReplicationLag: defaultMaxReplicationLag,
	}

	host, _ := params["host"].(string)
	if host == "" {
		return cfg, fmt.Errorf("postgresql: host is required")
	}
	cfg.Host = host

	if v, ok := types.IntParam(params, "port"); ok && v > 0 {
		cfg.Port = v
	}

	user, _ := params["username"].(string)
	if user == "" {
		return cfg, fmt.Errorf("postgresql: username is required")
	}
	cfg.Username = user

	pwd, _ := params["password"].(string)
	if pwd == "" {
		return cfg, fmt.Errorf("postgresql: password is required")
	}
	cfg.Password = pwd

	// `database` (singular) is what the probe this one replaced took, and
	// what most people write. Same meaning here: the database the
	// connection opens on.
	if v, ok := types.StringParam(params, "database"); ok && v != "" {
		cfg.Databases = []string{v}
	}

	if raw, ok := params["databases"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					cfg.Databases = append(cfg.Databases, s)
				}
			}
		case []string:
			cfg.Databases = v
		}
	}

	if v, ok := types.IntParam(params, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	if v, ok := types.DurationParam(params, "timeout"); ok && v > 0 {
		cfg.Timeout = v
	}

	if v, ok := types.DurationParam(params, "max_replication_lag_seconds"); ok && v >= 0 {
		cfg.MaxReplicationLag = v
	}

	if v, ok := types.StringParam(params, "sslmode"); ok && v != "" {
		if !sslModes[v] {
			return cfg, fmt.Errorf("postgresql: sslmode %q is not a libpq mode (disable, allow, prefer, require, verify-ca, verify-full)", v)
		}
		cfg.SSLMode = v
	}

	if v, ok := params["instance_name"].(string); ok {
		cfg.InstanceName = v
	}

	if raw, ok := asStringKeyedMap(params["tls"]); ok {
		tlsCfg := &pgTLSConfig{}
		if v, present := types.BoolParam(raw, "insecure_skip_verify"); present {
			tlsCfg.InsecureSkipVerify = v
		}
		if v, present := types.BoolParam(raw, "skip_verify"); present {
			tlsCfg.InsecureSkipVerify = v
		}
		if v, present := types.StringParam(raw, "ca_cert"); present && v != "" {
			tlsCfg.CACert = v
		}
		// The spelling the OTLP strategy and the mysql probe use.
		if v, present := types.StringParam(raw, "ca_file"); present && v != "" {
			tlsCfg.CACert = v
		}
		cfg.TLSConfig = tlsCfg
	}

	// libpq's own name for the same file. Accepted so a configuration
	// written against the previous probe keeps verifying against its CA
	// instead of silently falling back to the system roots.
	if v, ok := types.StringParam(params, "sslrootcert"); ok && v != "" {
		if cfg.TLSConfig == nil {
			cfg.TLSConfig = &pgTLSConfig{}
		}
		cfg.TLSConfig.CACert = v
	}

	return cfg, nil
}

// asStringKeyedMap normalises the two map shapes a YAML decode produces
// (yaml.v2 gives interface-keyed maps for nested blocks, which is why
// the tls block was previously read only when it arrived as the other
// shape).
func asStringKeyedMap(raw interface{}) (map[string]interface{}, bool) {
	switch m := raw.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = v
		}
		return out, true
	}
	return nil, false
}

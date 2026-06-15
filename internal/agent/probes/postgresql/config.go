package postgresql

import (
	"fmt"
	"time"
)

// config holds the validated probe configuration.
type config struct {
	Host      string
	Port      int
	Username  string
	Password  string
	Databases []string // empty = monitor server-wide stats only
	Interval  time.Duration
	TLSConfig *pgTLSConfig // nil = no TLS
}

// parseConfig converts the free-form params map from the probe YAML
// into a typed config. Required: host, username, password. Defaults:
// port=5432, interval=60s.
func parseConfig(params map[string]interface{}) (config, error) {
	cfg := config{
		Port:     5432,
		Interval: defaultInterval,
	}

	host, _ := params["host"].(string)
	if host == "" {
		return cfg, fmt.Errorf("postgresql: host is required")
	}
	cfg.Host = host

	if v, ok := params["port"].(int); ok && v > 0 {
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

	if v, ok := params["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	if raw, ok := params["tls"].(map[string]interface{}); ok {
		tlsCfg := &pgTLSConfig{}
		if v, ok := raw["insecure_skip_verify"].(bool); ok {
			tlsCfg.InsecureSkipVerify = v
		}
		if v, ok := raw["ca_cert"].(string); ok {
			tlsCfg.CACert = v
		}
		cfg.TLSConfig = tlsCfg
	}

	return cfg, nil
}

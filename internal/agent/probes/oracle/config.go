package oracle

import (
	"fmt"
	"senhub-agent.go/internal/agent/probes/types"
	"time"
)

// config is the parsed params block of an oracle probe entry.
type config struct {
	Host        string
	Port        int
	ServiceName string
	Username    string
	Password    string
	Interval    time.Duration
}

const (
	defaultPort     = 1521
	defaultInterval = 60 * time.Second
)

// instance is the unique id of the monitored database, used as the
// `instance` tag and the entity db.instance.id. It is the go-ora DSN
// shape without credentials: oracle://host:port/service.
func (c config) instance() string {
	return fmt.Sprintf("oracle://%s:%d/%s", c.Host, c.Port, c.ServiceName)
}

// parseConfig validates and normalises a probe params block. Config
// errors surface at construction so a misconfigured probe never starts.
func parseConfig(raw map[string]interface{}) (config, error) {
	cfg := config{Port: defaultPort, Interval: defaultInterval}

	host, ok := stringParam(raw, "host")
	if !ok || host == "" {
		return cfg, fmt.Errorf("oracle requires a non-empty host")
	}
	cfg.Host = host

	service, ok := stringParam(raw, "service_name")
	if !ok || service == "" {
		return cfg, fmt.Errorf("oracle requires a non-empty service_name")
	}
	cfg.ServiceName = service

	user, ok := stringParam(raw, "username")
	if !ok || user == "" {
		return cfg, fmt.Errorf("oracle requires a non-empty username")
	}
	cfg.Username = user

	pass, _ := stringParam(raw, "password")
	cfg.Password = pass

	if v, ok := intParam(raw, "port"); ok {
		if v <= 0 || v > 65535 {
			return cfg, fmt.Errorf("oracle port %d out of range", v)
		}
		cfg.Port = v
	}

	if v, ok := intParam(raw, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	return cfg, nil
}

// stringParam / intParam delegate to the shared helpers. They were
// private duplicates that had drifted: the local intParam accepted
// int/int64/float64/string, the shared one also handles the unsigned and
// int32 forms, and rejects a float that is not a whole number instead of
// truncating it silently (#831).
func stringParam(raw map[string]interface{}, key string) (string, bool) {
	return types.StringParam(raw, key)
}

func intParam(raw map[string]interface{}, key string) (int, bool) {
	return types.IntParam(raw, key)
}

package oracle

import (
	"fmt"
	"strconv"
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

func stringParam(raw map[string]interface{}, key string) (string, bool) {
	v, ok := raw[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// intParam accepts the int / int64 / float64 forms a YAML scalar can
// decode to depending on the loader path.
func intParam(raw map[string]interface{}, key string) (int, bool) {
	v, ok := raw[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i, true
		}
	}
	return 0, false
}

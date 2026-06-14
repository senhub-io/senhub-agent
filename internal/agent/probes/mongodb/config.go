package mongodb

import "time"

const (
	probeType = "mongodb"

	defaultURI               = "mongodb://localhost:27017"
	defaultTimeout           = 10 * time.Second
	defaultInterval          = 60 * time.Second
	defaultDirectConnection  = true
)

// config is the validated configuration for a mongodb probe instance.
type config struct {
	URI              string
	Timeout          time.Duration
	Interval         time.Duration
	DirectConnection bool
}

func parseConfig(raw map[string]interface{}) (*config, error) {
	cfg := &config{
		URI:              defaultURI,
		Timeout:          defaultTimeout,
		Interval:         defaultInterval,
		DirectConnection: defaultDirectConnection,
	}

	if v, ok := raw["uri"].(string); ok && v != "" {
		cfg.URI = v
	}
	if v, ok := raw["timeout"].(int); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["direct_connection"].(bool); ok {
		cfg.DirectConnection = v
	}

	return cfg, nil
}

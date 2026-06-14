package kafka

import (
	"fmt"
	"strings"
	"time"
)

const (
	// ProbeType is the canonical, stable type identifier.
	ProbeType = "kafka"

	defaultInterval        = 60 * time.Second
	defaultTimeout         = 10 * time.Second
	defaultProtocolVersion = "2.0.0"
)

// probeConfig holds the parsed, validated probe configuration.
type probeConfig struct {
	Brokers         []string
	ProtocolVersion string
	TLS             bool
	SASLMechanism   string // "", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"
	SASLUsername    string
	SASLPassword    string
	Interval        time.Duration
	Timeout         time.Duration
	TopicFilter     []string // globs; empty = all non-internal topics
	GroupFilter     []string // globs; empty = all groups
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: defaultProtocolVersion,
		Interval:        defaultInterval,
		Timeout:         defaultTimeout,
	}

	if v := stringSlice(raw["brokers"]); len(v) > 0 {
		cfg.Brokers = v
	}
	if v, ok := raw["protocol_version"].(string); ok && v != "" {
		cfg.ProtocolVersion = v
	}
	if v, ok := raw["tls"].(bool); ok {
		cfg.TLS = v
	}
	if v, ok := raw["sasl_mechanism"].(string); ok {
		mech := strings.ToUpper(v)
		switch mech {
		case "", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
			cfg.SASLMechanism = mech
		default:
			return cfg, fmt.Errorf("kafka: unsupported sasl_mechanism %q (valid: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", v)
		}
	}
	if v, ok := raw["sasl_username"].(string); ok {
		cfg.SASLUsername = v
	}
	if v, ok := raw["sasl_password"].(string); ok {
		cfg.SASLPassword = v
	}
	if secs, ok := raw["interval"].(int); ok && secs > 0 {
		cfg.Interval = time.Duration(secs) * time.Second
	}
	if secs, ok := raw["timeout"].(int); ok && secs > 0 {
		cfg.Timeout = time.Duration(secs) * time.Second
	}
	cfg.TopicFilter = stringSlice(raw["topic_filter"])
	cfg.GroupFilter = stringSlice(raw["group_filter"])

	if cfg.SASLMechanism != "" && (cfg.SASLUsername == "" || cfg.SASLPassword == "") {
		return cfg, fmt.Errorf("kafka: sasl_mechanism %q requires sasl_username and sasl_password", cfg.SASLMechanism)
	}

	return cfg, nil
}

// stringSlice coerces a YAML-decoded value into []string, dropping empties.
func stringSlice(raw interface{}) []string {
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

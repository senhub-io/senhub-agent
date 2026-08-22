package kafka

import (
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
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
	// InstanceName, when non-empty, overrides the tech-reported cluster id as
	// the service.instance.id for this probe's entity. Useful when multiple
	// probes monitor the same cluster from different vantage points and the
	// operator wants an explicit, stable name.
	InstanceName string
}

func parseConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Brokers:         []string{"localhost:9092"},
		ProtocolVersion: defaultProtocolVersion,
		Interval:        defaultInterval,
		Timeout:         defaultTimeout,
	}

	if v := types.StringSlice(raw["brokers"]); len(v) > 0 {
		cfg.Brokers = v
	}
	if v, ok := types.StringParam(raw, "protocol_version"); ok && v != "" {
		cfg.ProtocolVersion = v
	}
	if v, ok := types.BoolParam(raw, "tls"); ok {
		cfg.TLS = v
	}
	if v, ok := types.StringParam(raw, "sasl_mechanism"); ok {
		mech := strings.ToUpper(v)
		switch mech {
		case "", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
			cfg.SASLMechanism = mech
		default:
			return cfg, fmt.Errorf("kafka: unsupported sasl_mechanism %q (valid: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", v)
		}
	}
	if v, ok := types.StringParam(raw, "sasl_username"); ok {
		cfg.SASLUsername = v
	}
	if v, ok := types.StringParam(raw, "sasl_password"); ok {
		cfg.SASLPassword = v
	}
	if secs, ok := types.IntParam(raw, "interval"); ok && secs > 0 {
		cfg.Interval = time.Duration(secs) * time.Second
	}
	if secs, ok := types.IntParam(raw, "timeout"); ok && secs > 0 {
		cfg.Timeout = time.Duration(secs) * time.Second
	}
	cfg.TopicFilter = types.StringSlice(raw["topic_filter"])
	cfg.GroupFilter = types.StringSlice(raw["group_filter"])
	if v, ok := types.StringParam(raw, "instance_name"); ok {
		cfg.InstanceName = v
	}

	if cfg.SASLMechanism != "" && (cfg.SASLUsername == "" || cfg.SASLPassword == "") {
		return cfg, fmt.Errorf("kafka: sasl_mechanism %q requires sasl_username and sasl_password", cfg.SASLMechanism)
	}

	return cfg, nil
}

package ibmi

import (
	"errors"
	"fmt"
	"time"
)

// probeConfig is the typed view of the raw map[string]interface{} passed
// by the senhub-agent scheduler to every probe constructor. We validate
// and narrow the map into this struct once, at NewIBMiProbe time, so the
// Collect path can work on concrete values without runtime type assertions.
//
// Field layout:
//
//	host            string          IBM i hostname, required
//	user            string          IBM i user profile, required
//	password        string          password, required (read from env
//	                                indirection in production — the map
//	                                carries the already-resolved string)
//	interval        int             collection period in seconds
//	                                (optional, default 30)
//	bridge_runner_dir string        directory containing Jt400Runner.class
//	                                and jt400.jar, required
//	java_home       string          optional override for JAVA_HOME
//	query_timeout_s int             optional per-query timeout, default 10
//	startup_timeout_s int           optional bridge startup timeout, default 15
type probeConfig struct {
	Host            string
	User            string
	Password        string
	Interval        time.Duration
	BridgeRunnerDir string
	JavaHome        string
	// NativeRunner is an optional path to a GraalVM native-image
	// compiled jt400runner binary. When set, the bridge spawns it
	// directly instead of `java -cp ... Jt400Runner` — no JRE required
	// at runtime. bridge_runner_dir then becomes optional.
	NativeRunner   string
	QueryTimeout   time.Duration
	StartupTimeout time.Duration

	// EnabledCollectors is an optional allowlist of collector names to
	// activate. Empty = every collector in defaultCollectors() runs.
	// If set, only the listed names are kept.
	EnabledCollectors []string
	// DisabledCollectors is an optional denylist applied on top of
	// EnabledCollectors (or on top of the default set if EnabledCollectors
	// is empty). Useful for deployments that want "everything except
	// these two heavy queries".
	DisabledCollectors []string

	// MessageQueues is an optional list that expands the default
	// message_queue collector into multiple queue-scoped instances.
	// When nil or empty the probe instantiates a single QSYSOPR
	// collector (the historical behaviour). When set, the default
	// QSYSOPR collector is replaced by one collector per entry.
	MessageQueues []messageQueueSpec
}

// messageQueueSpec is the typed view of one entry under
// `message_queues` in the probe YAML configuration.
type messageQueueSpec struct {
	Library     string
	Name        string
	MinSeverity int
}

// parseProbeConfig converts the raw configuration map into a typed
// probeConfig, returning the first validation error encountered. The
// error messages are phrased so a misconfiguration in YAML can be
// diagnosed without reading the source.
func parseProbeConfig(raw map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Interval:       30 * time.Second,
		QueryTimeout:   10 * time.Second,
		StartupTimeout: 15 * time.Second,
	}

	var err error

	cfg.Host, err = requireString(raw, "host")
	if err != nil {
		return cfg, err
	}
	cfg.User, err = requireString(raw, "user")
	if err != nil {
		return cfg, err
	}
	cfg.Password, err = requireString(raw, "password")
	if err != nil {
		return cfg, err
	}
	// native_runner takes priority: if set, bridge_runner_dir + java_home
	// become optional (no JVM on the runtime path).
	if v, ok := raw["native_runner"]; ok {
		s, ok := v.(string)
		if !ok {
			return cfg, fmt.Errorf("config key %q: expected string, got %T", "native_runner", v)
		}
		cfg.NativeRunner = s
	}
	if cfg.NativeRunner == "" {
		cfg.BridgeRunnerDir, err = requireString(raw, "bridge_runner_dir")
		if err != nil {
			return cfg, err
		}
	} else if v, ok := raw["bridge_runner_dir"]; ok {
		// Optional when native_runner is set (used as working dir only).
		s, ok := v.(string)
		if !ok {
			return cfg, fmt.Errorf("config key %q: expected string, got %T", "bridge_runner_dir", v)
		}
		cfg.BridgeRunnerDir = s
	}

	if v, ok := raw["java_home"]; ok {
		s, ok := v.(string)
		if !ok {
			return cfg, fmt.Errorf("config key %q: expected string, got %T", "java_home", v)
		}
		cfg.JavaHome = s
	}
	if v, ok := raw["interval"]; ok {
		d, err := toSeconds(v, "interval")
		if err != nil {
			return cfg, err
		}
		cfg.Interval = d
	}
	if v, ok := raw["query_timeout_s"]; ok {
		d, err := toSeconds(v, "query_timeout_s")
		if err != nil {
			return cfg, err
		}
		cfg.QueryTimeout = d
	}
	if v, ok := raw["startup_timeout_s"]; ok {
		d, err := toSeconds(v, "startup_timeout_s")
		if err != nil {
			return cfg, err
		}
		cfg.StartupTimeout = d
	}
	if v, ok := raw["enabled_collectors"]; ok {
		list, err := toStringList(v, "enabled_collectors")
		if err != nil {
			return cfg, err
		}
		cfg.EnabledCollectors = list
	}
	if v, ok := raw["disabled_collectors"]; ok {
		list, err := toStringList(v, "disabled_collectors")
		if err != nil {
			return cfg, err
		}
		cfg.DisabledCollectors = list
	}
	if v, ok := raw["message_queues"]; ok {
		specs, err := parseMessageQueues(v)
		if err != nil {
			return cfg, err
		}
		cfg.MessageQueues = specs
	}

	return cfg, nil
}

// parseMessageQueues accepts the raw YAML structure for
// `message_queues`. Each entry is a map with keys library / name /
// min_severity. library defaults to QSYS when omitted; name is
// required; min_severity defaults to 0.
func parseMessageQueues(v interface{}) ([]messageQueueSpec, error) {
	items, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("config key %q: expected list of mappings, got %T", "message_queues", v)
	}
	specs := make([]messageQueueSpec, 0, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("config key %q[%d]: expected mapping, got %T", "message_queues", i, item)
		}
		spec := messageQueueSpec{Library: "QSYS"}
		if v, ok := entry["library"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("config key %q[%d].library: expected string, got %T", "message_queues", i, v)
			}
			spec.Library = s
		}
		if v, ok := entry["name"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("config key %q[%d].name: expected string, got %T", "message_queues", i, v)
			}
			spec.Name = s
		}
		if spec.Name == "" {
			return nil, fmt.Errorf("config key %q[%d]: %q is required", "message_queues", i, "name")
		}
		if v, ok := entry["min_severity"]; ok {
			switch n := v.(type) {
			case int:
				spec.MinSeverity = n
			case int64:
				spec.MinSeverity = int(n)
			case float64:
				spec.MinSeverity = int(n)
			default:
				return nil, fmt.Errorf("config key %q[%d].min_severity: expected number, got %T", "message_queues", i, v)
			}
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// toStringList accepts either []interface{} (YAML list → map) or
// []string (direct injection from tests). The YAML path is the
// common one; tests use the direct path.
func toStringList(v interface{}, key string) ([]string, error) {
	switch items := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(items))
		for i, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("config key %q[%d]: expected string, got %T", key, i, item)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return items, nil
	default:
		return nil, fmt.Errorf("config key %q: expected list of strings, got %T", key, v)
	}
}

func requireString(raw map[string]interface{}, key string) (string, error) {
	v, ok := raw[key]
	if !ok {
		return "", fmt.Errorf("config key %q is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("config key %q: expected string, got %T", key, v)
	}
	if s == "" {
		return "", errors.New("config key " + key + " must not be empty")
	}
	return s, nil
}

// toSeconds accepts either an int or float64 (YAML unmarshals integers as
// int when decoded into map[string]interface{}, but JSON decodes numbers
// as float64 — we handle both to be source-agnostic).
func toSeconds(v interface{}, key string) (time.Duration, error) {
	switch n := v.(type) {
	case int:
		return time.Duration(n) * time.Second, nil
	case int64:
		return time.Duration(n) * time.Second, nil
	case float64:
		return time.Duration(n) * time.Second, nil
	default:
		return 0, fmt.Errorf("config key %q: expected number (seconds), got %T", key, v)
	}
}

package process

import (
	"fmt"
	"regexp"
	"senhub-agent.go/internal/agent/probes/types"
	"time"
)

// config holds the validated process probe configuration.
type config struct {
	// byName is a compiled RE2 filter on process name (nil = accept all).
	byName *regexp.Regexp
	// byUser filters by OS username (empty = accept all).
	byUser string
	// topN, if positive, retains only the N processes with highest CPU.
	topN int
	// aggregate emits one extra rolled-up datapoint per distinct process name.
	aggregate bool
	// interval between collection cycles.
	interval time.Duration
}

// parseConfig validates and converts the raw YAML params block.
func parseConfig(raw map[string]interface{}) (config, error) {
	cfg := config{
		topN:      0,
		aggregate: true,
		interval:  30 * time.Second,
	}

	if v, ok := types.IntParam(raw, "interval"); ok && v > 0 {
		cfg.interval = time.Duration(v) * time.Second
	}

	if filter, ok := raw["filter"].(map[string]interface{}); ok {
		if s, ok := filter["by_name"].(string); ok && s != "" {
			re, err := regexp.Compile(s)
			if err != nil {
				return config{}, fmt.Errorf("filter.by_name: %w", err)
			}
			cfg.byName = re
		}
		if s, ok := filter["by_user"].(string); ok {
			cfg.byUser = s
		}
		if n, ok := types.IntParam(filter, "top_n"); ok {
			cfg.topN = n
		}
	}

	if agg, ok := raw["aggregate"].(map[string]interface{}); ok {
		if en, ok := agg["enabled"].(bool); ok {
			cfg.aggregate = en
		}
	}

	return cfg, nil
}

// detailed reports whether the per-process series are emitted. The
// operator has either named what to watch, or bounded the sample with
// top_n. An unfiltered view is every process on the machine, and the
// identity of those series carries the process id, so it grows by one
// set per process start and never shrinks: measured at about four and a
// half thousand series on an ordinary machine, growing by fifty a
// minute. That view gets the per-name roll-up alone.
//
// It is the rule the entity source already followed, for the same
// reason, on the other rail.
func (c config) detailed() bool {
	return c.byName != nil || c.byUser != "" || c.topN > 0
}

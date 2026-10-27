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
	// topN, if positive, retains only the N processes ranking highest on topBy.
	topN int
	// topBy is what top_n ranks on: "cpu" (default) or "memory".
	topBy string
	// aggregate emits one extra rolled-up datapoint per distinct process name.
	aggregate bool
	// perPID keeps one series per process under top_n instead of the per-name
	// roll-up. Process ids are not stable, so those series churn.
	perPID bool
	// interval between collection cycles.
	interval time.Duration
}

const (
	topByCPU    = "cpu"
	topByMemory = "memory"
)

// parseConfig validates and converts the raw YAML params block.
func parseConfig(raw map[string]interface{}) (config, error) {
	cfg := config{
		topN:      0,
		topBy:     topByCPU,
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
		if b, ok := filter["per_pid"].(bool); ok {
			cfg.perPID = b
		}
		if s, ok := filter["top_by"].(string); ok && s != "" {
			if s != topByCPU && s != topByMemory {
				return config{}, fmt.Errorf("filter.top_by: %q is neither %q nor %q", s, topByCPU, topByMemory)
			}
			cfg.topBy = s
		}
	}

	if agg, ok := raw["aggregate"].(map[string]interface{}); ok {
		if en, ok := agg["enabled"].(bool); ok {
			cfg.aggregate = en
		}
	}

	return cfg, nil
}

// detailed reports whether the per-process series are emitted. The operator
// has either named what to watch (by_name, by_user), or asked for the
// per-process detail of a top_n sample with per_pid. Without per_pid a top_n
// sample is reported as the per-name roll-up: the membership of a top set
// changes all the time, and every process id and creation time it exposed
// became a new series. An unfiltered view is every process on the machine and
// gets the roll-up alone for the same reason.
func (c config) detailed() bool {
	if c.topN > 0 {
		return c.perPID
	}
	return c.byName != nil || c.byUser != ""
}

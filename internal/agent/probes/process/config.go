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
	// perPID keeps the per-process series under top_n. Every series then
	// carries the process id and creation time, so the series set grows
	// with each process that enters the top.
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

	if det, ok := raw["detail"].(map[string]interface{}); ok {
		if v, ok := det["per_pid"].(bool); ok {
			cfg.perPID = v
		}
	}

	return cfg, nil
}

// topByName reports whether top_n ranks process names and emits the
// per-name roll-up, the stable view, rather than per-process series.
func (c config) topByName() bool {
	return c.topN > 0 && !c.perPID
}

// detailed reports whether the per-process series are emitted. The
// operator has named what to watch, or bounded the sample with top_n and
// asked for per-process series with detail.per_pid. Under top_n alone the
// top is ranked by process name and reported as the roll-up: the members
// of a top change as processes start, so series keyed on the process id
// never stop appearing. An unfiltered view is every process on the machine, and the
// identity of those series carries the process id, so it grows by one
// set per process start and never shrinks: measured at about four and a
// half thousand series on an ordinary machine, growing by fifty a
// minute. That view gets the per-name roll-up alone.
//
// It is the rule the entity source already followed, for the same
// reason, on the other rail.
func (c config) detailed() bool {
	if c.topN > 0 {
		return c.perPID
	}
	return c.byName != nil || c.byUser != ""
}

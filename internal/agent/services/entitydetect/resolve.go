package entitydetect

import (
	"net"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/governance"
)

// DefaultInterval is the heartbeat cadence when nothing names one. It
// matches what the OTLP entities signal used, so moving the producer out
// of that strategy changes no existing install's behaviour.
const DefaultInterval = 5 * time.Minute

const defaultDependsOnDebounce = 3

// Resolve decides whether entity detection runs and how.
//
// Two sources, in order:
//
//  1. the global `entities:` block, which is the operator saying what
//     this host should describe about itself, whatever consumes it;
//  2. failing that, an OTLP output's `signals.entities` — where this
//     configuration used to live. Reading it keeps every existing
//     install behaving exactly as before, so nobody loses entity
//     emission by upgrading.
//
// An agent with neither produces nothing, which is what an install that
// never asked for entities already did.
func Resolve(global *configuration.EntitiesConfig, storage []configuration.StorageConfig, agentInstanceID string) Config {
	cfg := Config{
		Interval:          DefaultInterval,
		DependsOnDebounce: defaultDependsOnDebounce,
		AgentInstanceID:   agentInstanceID,
		AgentService:      "senhub-agent",
		AgentVersion:      cliArgs.Version,
	}

	if e := global; e != nil {
		cfg.Enabled = e.Enabled
		if d, err := time.ParseDuration(e.Interval); err == nil && d > 0 {
			cfg.Interval = d
		}
		if d := e.DependsOn; d != nil {
			cfg.DependsOnEnabled = d.Enabled
			if d.Debounce > 0 {
				cfg.DependsOnDebounce = d.Debounce
			}
			cfg.DependsOnExcludeCIDRs = parseCIDRs(d.ExcludeCIDRs)
		}
		return cfg
	}

	applyOTLPFallback(&cfg, storage)
	return cfg
}

// applyOTLPFallback reads the settings from the first OTLP output that
// turns entities on, which is where they lived before they had a home of
// their own.
func applyOTLPFallback(cfg *Config, storage []configuration.StorageConfig) {
	for _, s := range storage {
		if s.Name != "otlp" {
			continue
		}
		signals, ok := s.Params["signals"].(map[string]interface{})
		if !ok {
			continue
		}
		entities, ok := signals["entities"].(map[string]interface{})
		if !ok {
			continue
		}
		enabled, _ := entities["enabled"].(bool)
		if !enabled {
			continue
		}
		cfg.Enabled = true
		if raw, ok := entities["interval"].(string); ok {
			if d, err := time.ParseDuration(raw); err == nil && d > 0 {
				cfg.Interval = d
			}
		}
		if on, ok := entities["depends_on_enabled"].(bool); ok {
			cfg.DependsOnEnabled = on
		}
		if n, ok := readInt(entities["depends_on_debounce"]); ok && n > 0 {
			cfg.DependsOnDebounce = n
		}
		if list, ok := entities["depends_on_exclude_cidrs"].([]interface{}); ok {
			var raw []string
			for _, v := range list {
				if str, isStr := v.(string); isStr {
					raw = append(raw, str)
				}
			}
			cfg.DependsOnExcludeCIDRs = parseCIDRs(raw)
		}
		if gov, err := governance.Parse(entities["governance"]); err == nil {
			cfg.Governance = gov
		}
		return
	}
}

// parseCIDRs drops what does not parse rather than refusing the whole
// list: a typo in a privacy filter must not take entity detection down
// with it, and the detector logs what it emits either way.
func parseCIDRs(raw []string) []*net.IPNet {
	var out []*net.IPNet
	for _, entry := range raw {
		if _, n, err := net.ParseCIDR(entry); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func readInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

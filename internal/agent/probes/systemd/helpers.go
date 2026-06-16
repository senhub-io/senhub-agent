// Package systemd monitors systemd units via the D-Bus API.
//
// Pure, platform-independent helpers live here so they compile and
// test on every OS. The dbus connection and linux-only init system
// access live in systemd_probe.go (//go:build linux).
package systemd

import (
	"path/filepath"
	"strings"
	"time"

	dbus "github.com/coreos/go-systemd/v22/dbus"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// defaultIncludeTypes is the set of unit type suffixes enabled when
// include_types is not specified.
var defaultIncludeTypes = []string{"service", "socket", "timer", "mount"}

// probeConfig holds the parsed operator config.
type probeConfig struct {
	Units        []string
	IncludeTypes map[string]bool
	Interval     time.Duration
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Interval:     30 * time.Second,
		IncludeTypes: make(map[string]bool),
	}

	// units: explicit list or globs
	if raw, ok := config["units"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				cfg.Units = append(cfg.Units, s)
			}
		}
	}

	// include_types
	if raw, ok := config["include_types"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				cfg.IncludeTypes[strings.ToLower(s)] = true
			}
		}
	}
	if len(cfg.IncludeTypes) == 0 {
		for _, t := range defaultIncludeTypes {
			cfg.IncludeTypes[t] = true
		}
	}

	// interval
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	return cfg, nil
}

// unitTypeSuffix extracts the type from the unit name (e.g. "service"
// from "nginx.service", "timer" from "logrotate.timer").
func unitTypeSuffix(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return ""
}

// filterUnits applies the include_types filter and, when units is
// non-empty, restricts to units matching at least one glob pattern.
func filterUnits(cfg probeConfig, all []dbus.UnitStatus) []dbus.UnitStatus {
	var out []dbus.UnitStatus
	for _, u := range all {
		unitType := unitTypeSuffix(u.Name)
		if !cfg.IncludeTypes[unitType] {
			continue
		}
		if len(cfg.Units) == 0 {
			out = append(out, u)
			continue
		}
		for _, pattern := range cfg.Units {
			if matched, _ := filepath.Match(pattern, u.Name); matched {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

// buildDatapoints emits the per-unit metrics for one unit.
//
// nRestarts is non-nil only for service units when the NRestarts
// property was successfully retrieved from dbus (linux only). Callers
// on non-linux platforms or in tests that don't have a live dbus
// connection pass nil to omit the restart counter datapoint.
func buildDatapoints(u dbus.UnitStatus, ts time.Time, nRestarts *float64) []data_store.DataPoint {
	unitType := unitTypeSuffix(u.Name)
	baseTags := []tags.Tag{
		{Key: "systemd.unit", Value: u.Name},
		{Key: "systemd.unit.type", Value: unitType},
		{Key: "metric_type", Value: "status"},
	}

	// systemd.unit.active_state: 1=active, 0=otherwise
	var activeVal float64
	if u.ActiveState == "active" {
		activeVal = 1
	}
	points := []data_store.DataPoint{
		{Name: "systemd.unit.active_state", Value: activeVal, Timestamp: ts, Tags: baseTags},
	}

	// systemd.unit.sub_state: 1=running|listening, 0=dead|exited
	// Carry sub_state as a tag so the operator can filter.
	subTags := append(append([]tags.Tag(nil), baseTags...), tags.Tag{Key: "sub_state", Value: u.SubState})
	var subVal float64
	switch u.SubState {
	case "running", "listening":
		subVal = 1
	}
	points = append(points, data_store.DataPoint{Name: "systemd.unit.sub_state", Value: subVal, Timestamp: ts, Tags: subTags})

	// systemd.unit.load_state: 1=loaded, 0=not-found|error
	var loadVal float64
	if u.LoadState == "loaded" {
		loadVal = 1
	}
	points = append(points, data_store.DataPoint{Name: "systemd.unit.load_state", Value: loadVal, Timestamp: ts, Tags: baseTags})

	// systemd.unit.restarts: only for service units, only when the
	// NRestarts property was available (linux with live dbus).
	if unitType == "service" && nRestarts != nil {
		points = append(points, data_store.DataPoint{Name: "systemd.unit.restarts", Value: *nRestarts, Timestamp: ts, Tags: baseTags})
	}

	return points
}

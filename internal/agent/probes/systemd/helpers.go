// Package systemd monitors systemd units via the D-Bus API.
//
// Pure, platform-independent helpers live here so they compile and
// test on every OS. The dbus connection and linux-only init system
// access live in systemd_probe.go (//go:build linux).
package systemd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/hostservice"

	dbus "github.com/coreos/go-systemd/v22/dbus"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Detail levels available to operators. The default keeps the probe
// cheap in series volume; full restores the pre-detail metric set.
const (
	systemdDetailState = "state"
	systemdDetailFull  = "full"
)

// defaultIncludeTypes is the set of unit type suffixes enabled when
// include_types is not specified.
var defaultIncludeTypes = []string{"service", "socket", "timer", "mount"}

// probeConfig holds the parsed operator config.
type probeConfig struct {
	Units        []string
	IncludeTypes map[string]bool
	Detail       string
	Interval     time.Duration
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Interval:     30 * time.Second,
		IncludeTypes: make(map[string]bool),
		Detail:       systemdDetailState,
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

	// detail
	if raw, ok := config["detail"]; ok {
		if s, ok := raw.(string); ok && s != "" {
			if s != systemdDetailState && s != systemdDetailFull {
				return cfg, fmt.Errorf("systemd: detail must be %q or %q, got %q", systemdDetailState, systemdDetailFull, s)
			}
			cfg.Detail = s
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

// sessionUnitPatterns match the units systemd creates for each user login
// and removes at logout (run-user-1000.mount, user@1000.service,
// user-runtime-dir@1000.service). They are plumbing of a session, not
// services to watch, and every SSH login would otherwise add series that
// stop the moment it ends. They are left out of the default view only;
// a units list can still name them.
var sessionUnitPatterns = []string{"run-user-*.mount", "user@*.service", "user-runtime-dir@*.service"}

func isSessionUnit(name string) bool {
	for _, p := range sessionUnitPatterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// selectEnabledByDefault applies the default policy: with no explicit
// units list, only units whose UnitFileState is enabled or
// enabled-runtime are selected. A missing or unreadable UnitFileState
// is not selected.
func selectEnabledByDefault(candidates []dbus.UnitStatus, unitFileStates map[string]string) []dbus.UnitStatus {
	out := make([]dbus.UnitStatus, 0, len(candidates))
	for _, u := range candidates {
		unitFileState, ok := unitFileStates[u.Name]
		if !ok {
			continue
		}
		if unitFileState != "enabled" && unitFileState != "enabled-runtime" {
			continue
		}
		out = append(out, u)
	}
	return out
}

// filterUnits applies the selection policy. An explicit units list
// selects the units of the included types matching at least one glob
// pattern, whatever their session or UnitFileState. With
// an empty units list it restricts to the included types, removes
// session units, and keeps only enabled units.
func filterUnits(cfg probeConfig, all []dbus.UnitStatus, unitFileStates map[string]string) []dbus.UnitStatus {
	if len(cfg.Units) == 0 {
		candidates := make([]dbus.UnitStatus, 0, len(all))
		for _, u := range all {
			unitType := unitTypeSuffix(u.Name)
			if !cfg.IncludeTypes[unitType] {
				continue
			}
			if isSessionUnit(u.Name) {
				continue
			}
			candidates = append(candidates, u)
		}
		return selectEnabledByDefault(candidates, unitFileStates)
	}

	var out []dbus.UnitStatus
	for _, u := range all {
		if !cfg.IncludeTypes[unitTypeSuffix(u.Name)] {
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

// baseTagsForUnit builds the tag set common to every per-service datapoint
// for a systemd unit. The entity identity is host.id + host.service.name;
// host.name is carried as a descriptive tag. While host.id is unknown, the
// identity tags are omitted.
func baseTagsForUnit(u dbus.UnitStatus, hostID, hostName string) []tags.Tag {
	unitType := unitTypeSuffix(u.Name)
	t := []tags.Tag{
		{Key: "systemd.unit", Value: u.Name},
		{Key: "systemd.unit.type", Value: unitType},
		{Key: "metric_type", Value: "status"},
	}
	if hostID != "" {
		t = append(t,
			tags.Tag{Key: "host.id", Value: hostID},
			tags.Tag{Key: "host.service.name", Value: u.Name},
		)
	}
	if hostName != "" {
		t = append(t, tags.Tag{Key: "host.name", Value: hostName})
	}
	return t
}

// buildCheckResultDatapoint emits the probe-level check result gauge with
// the supplied base tags. If metric_type is not present, it is set to
// "status"; if it is already present, the caller's value is preserved.
func buildCheckResultDatapoint(ts time.Time, err error, baseTags []tags.Tag) data_store.DataPoint {
	tagsCopy := append([]tags.Tag(nil), baseTags...)

	hasMetricType := false
	for _, t := range tagsCopy {
		if t.Key == "metric_type" {
			hasMetricType = true
			break
		}
	}
	if !hasMetricType {
		tagsCopy = append(tagsCopy, tags.Tag{Key: "metric_type", Value: "status"})
	}

	return data_store.DataPoint{
		Name:      dbcommon.CheckResultMetric,
		Value:     float64(dbcommon.ClassifyError(err)),
		Timestamp: ts,
		Tags:      tagsCopy,
	}
}

// buildDatapoints emits the per-unit metrics for one unit. detail=state
// emits only active_state; detail=full emits active_state, sub_state,
// load_state, and restarts for service units when NRestarts is known.
func buildDatapoints(u dbus.UnitStatus, ts time.Time, nRestarts *float64, hostID, hostName, detail string) []data_store.DataPoint {
	unitType := unitTypeSuffix(u.Name)
	baseTags := baseTagsForUnit(u, hostID, hostName)

	points := []data_store.DataPoint{
		{Name: hostservice.RunningMetric, Value: hostservice.RunningSystemd(u.ActiveState), Timestamp: ts, Tags: baseTags},
	}

	if detail != systemdDetailFull {
		return points
	}

	// systemd.unit.active_state: 1=active, 0=otherwise
	var activeVal float64
	if u.ActiveState == "active" {
		activeVal = 1
	}
	points = append(points, data_store.DataPoint{Name: "systemd.unit.active_state", Value: activeVal, Timestamp: ts, Tags: baseTags})

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

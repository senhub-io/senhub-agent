package configuration

import (
	"maps"

	"senhub-agent.go/internal/agent/services/extension"
)

// overlayTarget lets an extension overlay change a freshly loaded
// configuration through the narrow extension.OverlayTarget surface.
type overlayTarget struct {
	cfg *LocalConfigurationData
}

// applyOverlays runs the registered overlays on cfg. Probe parameters are
// copied before the first change so a map shared with the previous
// configuration is never written.
func applyOverlays(cfg *LocalConfigurationData) {
	extension.ApplyOverlays(&overlayTarget{cfg: cfg})
}

func (t *overlayTarget) Probes() []extension.OverlayProbe {
	out := make([]extension.OverlayProbe, 0, len(t.cfg.Probes))
	for _, p := range t.cfg.Probes {
		seconds, _ := wholeSeconds(p.Params["interval"])
		out = append(out, extension.OverlayProbe{
			Name:            p.Name,
			Type:            p.Type,
			DisabledLocally: p.Enabled != nil && !*p.Enabled,
			Interval:        seconds,
		})
	}
	return out
}

func (t *overlayTarget) SetProbeEnabled(name string, enabled bool) bool {
	i := t.probeIndex(name)
	if i < 0 {
		return false
	}
	t.cfg.Probes[i].Enabled = &enabled
	return true
}

func (t *overlayTarget) SetProbeInterval(name string, seconds int) bool {
	i := t.probeIndex(name)
	if i < 0 {
		return false
	}
	params := maps.Clone(t.cfg.Probes[i].Params)
	if params == nil {
		params = ProbeConfigParams{}
	}
	params["interval"] = seconds
	t.cfg.Probes[i].Params = params
	return true
}

func (t *overlayTarget) Entities() extension.EntitySwitches {
	e := t.cfg.Entities
	if e == nil {
		return extension.EntitySwitches{}
	}
	return extension.EntitySwitches{
		Enabled:     e.Enabled,
		DependsOn:   e.DependsOn != nil && e.DependsOn.Enabled,
		Neighbors:   e.Neighbors != nil && e.Neighbors.Enabled,
		Aggregation: e.Aggregation != nil && e.Aggregation.Enabled,
	}
}

func (t *overlayTarget) SetEntities(s extension.EntitySwitches) {
	var e EntitiesConfig
	if t.cfg.Entities != nil {
		e = *t.cfg.Entities
	}
	e.Enabled = s.Enabled
	e.DependsOn = withDependsOn(e.DependsOn, s.DependsOn)
	e.Neighbors = withNeighbors(e.Neighbors, s.Neighbors)
	e.Aggregation = withAggregation(e.Aggregation, s.Aggregation)
	t.cfg.Entities = &e
}

func (t *overlayTarget) probeIndex(name string) int {
	for i, p := range t.cfg.Probes {
		if p.Name == name {
			return i
		}
	}
	return -1
}

func withDependsOn(c *EntitiesDependsOnConfig, enabled bool) *EntitiesDependsOnConfig {
	var out EntitiesDependsOnConfig
	if c != nil {
		out = *c
	}
	out.Enabled = enabled
	return &out
}

func withNeighbors(c *EntitiesNeighborsConfig, enabled bool) *EntitiesNeighborsConfig {
	var out EntitiesNeighborsConfig
	if c != nil {
		out = *c
	}
	out.Enabled = enabled
	return &out
}

func withAggregation(c *EntitiesAggregationConfig, enabled bool) *EntitiesAggregationConfig {
	var out EntitiesAggregationConfig
	if c != nil {
		out = *c
	}
	out.Enabled = enabled
	return &out
}

// wholeSeconds reads an interval written as a whole number, whatever
// numeric type the YAML decoder gave it.
func wholeSeconds(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		if float64(int(n)) == n {
			return int(n), true
		}
	}
	return 0, false
}

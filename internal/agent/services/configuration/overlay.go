package configuration

import (
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/logredact"
)

// minOverlayInterval and maxOverlayInterval bound the interval an
// overlay may set, matching the signed overlay whitelist.
const (
	minOverlayInterval = 10
	maxOverlayInterval = 3600
)

// overlayTarget lets an extension overlay change a freshly loaded
// configuration through the narrow extension.OverlayTarget surface.
// Every mutation records how to undo it, so a panicking overlay leaves
// the configuration exactly as it was.
type overlayTarget struct {
	cfg        *LocalConfigurationData
	configPath string
	undo       []func()
}

// applyOverlays runs the registered overlays on cfg, one extension at a
// time. A panicking overlay is recovered, logged at Error with the
// extension name and rolled back, so it can neither crash the agent
// during a reload nor leave half of its changes behind. Probe
// parameters are copied before the first change so a map shared with
// the previous configuration is never written.
func applyOverlays(cfg *LocalConfigurationData, configPath string) {
	for _, e := range extension.Registered() {
		if e.Overlay == nil {
			continue
		}
		t := &overlayTarget{cfg: cfg, configPath: configPath}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.rollback()
					log.Error().Str("extension", e.Name).Any("panic", r).Msg("Extension overlay panicked; its changes were discarded")
				}
			}()
			e.Overlay(t)
		}()
	}
}

// rollback undoes every change this overlay made, last first.
func (t *overlayTarget) rollback() {
	for i := len(t.undo) - 1; i >= 0; i-- {
		t.undo[i]()
	}
	t.undo = nil
}

func (t *overlayTarget) Probes() []extension.OverlayProbe {
	names := make([]string, len(t.cfg.Probes))
	for i, p := range t.cfg.Probes {
		names[i] = p.Name
	}
	envDefined := EnvProbeSources(names)
	reported, secretBearing, _ := reportedProbes(t.configPath)
	out := make([]extension.OverlayProbe, 0, len(t.cfg.Probes))
	for _, p := range t.cfg.Probes {
		seconds, _ := wholeSeconds(p.Params["interval"])
		out = append(out, extension.OverlayProbe{
			Name:            p.Name,
			Type:            p.Type,
			DisabledLocally: p.Enabled != nil && !*p.Enabled,
			Interval:        seconds,
			EnvDefined:      len(envDefined[p.Name]) > 0,
			Params:          reported[p.Name],
			SecretParams:    secretBearing[p.Name],
		})
	}
	return out
}

func reportedProbes(configPath string) (masked map[string]map[string]any, secretBearing map[string][]string, ok bool) {
	if configPath == "" {
		return nil, nil, false
	}
	data, err := loadMerged(configPath, nil)
	if err != nil {
		return nil, nil, false
	}
	masked = make(map[string]map[string]any, len(data.Probes))
	secretBearing = make(map[string][]string, len(data.Probes))
	for _, p := range data.Probes {
		raw := map[string]any(p.Params)
		masked[p.Name] = maskReported(raw)
		secretBearing[p.Name] = secretBearingParams(raw)
	}
	return masked, secretBearing, true
}

func maskReported(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = maskReportedValue(k, v)
	}
	return out
}

// wholeReferencePattern matches a value that is one whole reference and
// nothing else, for the schemes the agent can resolve.
var wholeReferencePattern = regexp.MustCompile(`^\$\{(env|secret|file):[^}]+\}$`)

// isAllowedReference reports whether s is a single whole reference of an
// allowed scheme without a default, so it can be reported as-is.
func isAllowedReference(s string) bool {
	return wholeReferencePattern.MatchString(s) && !strings.Contains(s, ":-")
}

func maskReportedValue(key string, v any) any {
	if v != nil && reportSecretKey(key) {
		if s, isString := v.(string); !isString || !isAllowedReference(s) {
			return "***"
		}
	}
	switch value := v.(type) {
	case string:
		if isAllowedReference(value) {
			return value
		}
		if strings.Contains(value, "${") {
			return "***"
		}
		return logredact.String(value)
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, child := range value {
			out[k] = maskReportedValue(k, child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = maskReportedValue(key, child)
		}
		return out
	default:
		return v
	}
}

// reportSecretKeyPattern widens secretFieldPattern for what leaves the
// host: names such as pass, pwd or auth carry secrets too.
var reportSecretKeyPattern = regexp.MustCompile(`(?i)(pass|pwd|private|auth|cookie|session|apikey)`)

func reportSecretKey(key string) bool {
	return secretFieldPattern.MatchString(key) || reportSecretKeyPattern.MatchString(key)
}

// secretBearingParams returns the sorted names of the top-level parameters
// that carry or may carry a secret: a secret-like name with a value, a
// reference, or a value the log redaction changes.
func secretBearingParams(raw map[string]any) []string {
	var out []string
	for k, v := range raw {
		if v == nil {
			continue
		}
		if valueHasSecret(k, v) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func valueHasSecret(key string, v any) bool {
	if v == nil {
		return false
	}
	if reportSecretKey(key) {
		return true
	}
	switch value := v.(type) {
	case string:
		return strings.Contains(value, "${") || logredact.String(value) != value
	case map[string]any:
		for k, child := range value {
			if valueHasSecret(k, child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if valueHasSecret(key, child) {
				return true
			}
		}
	}
	return false
}

func (t *overlayTarget) SetProbeEnabled(name string, enabled bool) bool {
	i := t.probeIndex(name)
	if i < 0 {
		return false
	}
	// The local file wins: a probe it disables stays disabled.
	if enabled && t.cfg.Probes[i].Enabled != nil && !*t.cfg.Probes[i].Enabled {
		return false
	}
	prev := t.cfg.Probes[i].Enabled
	t.undo = append(t.undo, func() { t.cfg.Probes[i].Enabled = prev })
	t.cfg.Probes[i].Enabled = &enabled
	return true
}

func (t *overlayTarget) SetProbeInterval(name string, seconds int) bool {
	i := t.probeIndex(name)
	if i < 0 || seconds < minOverlayInterval || seconds > maxOverlayInterval {
		return false
	}
	prev := t.cfg.Probes[i].Params
	t.undo = append(t.undo, func() { t.cfg.Probes[i].Params = prev })
	params := maps.Clone(prev)
	if params == nil {
		params = ProbeConfigParams{}
	}
	params["interval"] = seconds
	t.cfg.Probes[i].Params = params
	return true
}

func (t *overlayTarget) AddProbe(p extension.ManagedProbe) bool {
	if p.Name == "" || p.Type == "" {
		return false
	}
	if p.Interval != 0 && (p.Interval < minOverlayInterval || p.Interval > maxOverlayInterval) {
		return false
	}
	if t.probeIndex(p.Name) >= 0 {
		return false
	}
	if _, ok := p.Params["interval"]; ok {
		return false
	}
	params := make(ProbeConfigParams, len(p.Params)+1)
	for k, v := range p.Params {
		params[k] = v
	}
	if p.Interval != 0 {
		params["interval"] = p.Interval
	}
	idx := len(t.cfg.Probes)
	t.undo = append(t.undo, func() {
		t.cfg.Probes = append(t.cfg.Probes[:idx], t.cfg.Probes[idx+1:]...)
	})
	t.cfg.Probes = append(t.cfg.Probes, ProbeConfig{
		Name:    p.Name,
		Type:    p.Type,
		Enabled: &p.Enabled,
		Params:  params,
	})
	return true
}

func (t *overlayTarget) AdoptProbe(p extension.ManagedProbe) bool {
	i := t.probeIndex(p.Name)
	if i < 0 {
		return false
	}
	local := t.cfg.Probes[i]
	if local.Type != p.Type {
		return false
	}
	if local.Enabled != nil && !*local.Enabled {
		return false
	}
	if len(EnvProbeSources([]string{p.Name})[p.Name]) > 0 {
		return false
	}
	if p.Interval != 0 && (p.Interval < minOverlayInterval || p.Interval > maxOverlayInterval) {
		return false
	}
	if _, ok := p.Params["interval"]; ok {
		return false
	}
	// A remote overlay must not retarget a local probe while a local
	// secret-bearing parameter is left behind: the secret would be sent
	// to the new target. When the file cannot be read the secret-bearing
	// list is unknown, so a retarget cannot be proven safe and is refused.
	_, secretBearing, ok := reportedProbes(t.configPath)
	hasTargetParam := false
	for k := range p.Params {
		if extension.IsTargetParam(k) {
			hasTargetParam = true
			break
		}
	}
	if hasTargetParam && !ok {
		return false
	}
	if hasTargetParam {
		for _, name := range secretBearing[p.Name] {
			if _, ok := p.Params[name]; !ok {
				return false
			}
		}
	}
	prev := t.cfg.Probes[i]
	t.undo = append(t.undo, func() { t.cfg.Probes[i] = prev })
	params := maps.Clone(prev.Params)
	if params == nil {
		params = ProbeConfigParams{}
	}
	for k, v := range p.Params {
		params[k] = v
	}
	if p.Interval != 0 {
		params["interval"] = p.Interval
	}
	adopted := prev
	enabled := p.Enabled
	adopted.Enabled = &enabled
	adopted.Params = params
	t.cfg.Probes[i] = adopted
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
	prev := t.cfg.Entities
	t.undo = append(t.undo, func() { t.cfg.Entities = prev })
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

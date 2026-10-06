package configuration

// env_probes.go lets an operator declare and tune probes from environment
// variables, by one generic rule, for every way an agent is started
// (container, systemd unit, Helm chart, Podman) without a file to place:
//
//	SENHUB_PROBE_<NAME>_TYPE=<probe type>      declares a probe named <name>
//	SENHUB_PROBE_<NAME>_<PARAM>=value          sets one of its parameters
//	SENHUB_PROBE_<NAME>_<BLOCK>__<PARAM>=value sets a nested parameter
//	SENHUB_PROBE_<NAME>_<PARAM>_FILE=/path     reads the value from a file
//
// The environment is applied on top of the file configuration, after the
// multi-file merge and before ${...} substitution: a probe of the same name
// in a file has its parameters overridden one by one, a name found nowhere
// else creates the probe. Values are typed from the probe's parameter
// schema, so a mistake stops the agent at load with the variable named.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/logger"
)

// EnvProbePrefix starts every variable of the rule. The plural
// SENHUB_PROBES, the container's YAML fragment, does not match it.
const EnvProbePrefix = "SENHUB_PROBE_"

const fileSuffix = "_file"

// probeTypeLookup tells the loader whether a probe type is built into this
// binary. The registry lives above this package, so the application hands
// it over once at start-up; without it a type is judged by its schema alone.
var probeTypeLookup func(probeType string) bool

// SetProbeTypeLookup wires the probe registry into the environment rule.
func SetProbeTypeLookup(known func(probeType string) bool) { probeTypeLookup = known }

// probeLevelFields are the keys of a probe entry beside its params. A
// variable whose first segment is one of them sets the entry, not a
// parameter; PARAMS__<KEY> reaches a parameter that bears the same name.
var probeLevelFields = map[string]bool{
	"type": true, "enabled": true, "log_strategies": true,
	"custom_tags": true, "governance": true,
}

type envSetting struct {
	variable string
	segments []string // lowercased, split on "__"
	value    string
}

type envProbe struct {
	name     string
	settings []envSetting
}

// EnvProbeVariables lists, sorted, the variables of the rule set in the
// process environment, for `config show` to say where a probe came from.
// Names only: no value ever leaves this function.
func EnvProbeVariables() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && v != "" && strings.HasPrefix(k, EnvProbePrefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// EnvProbeSources maps each of the named probes to the SENHUB_PROBE_*
// variables that declare it or set one of its parameters, for the console
// to treat it as read-only. A probe no variable reaches is absent. Names
// only, never values.
func EnvProbeSources(probeNames []string) map[string][]string {
	groups, err := parseEnvProbes(os.Environ())
	if err != nil {
		return nil
	}
	out := map[string][]string{}
	for _, g := range groups {
		for _, name := range probeNames {
			if nameKey(name) != g.name {
				continue
			}
			for _, s := range g.settings {
				out[name] = append(out[name], s.variable)
			}
			sort.Strings(out[name])
		}
	}
	return out
}

// applyEnvProbes merges the probes the process environment declares into
// data. It is a pure function of environ and of the files it is asked to
// read (_FILE variables), so the raw and the resolved view of `config show`
// see the same probes.
func applyEnvProbes(data *LocalConfigurationData, environ []string, log *logger.ModuleLogger) error {
	groups, err := parseEnvProbes(environ)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err := mergeEnvProbe(data, g, log); err != nil {
			return err
		}
	}
	return nil
}

// parseEnvProbes groups the variables of the rule by probe name. An empty
// value counts as unset, since container platforms commonly define every
// variable of a template and leave the unused ones blank.
func parseEnvProbes(environ []string) ([]envProbe, error) {
	byName := map[string]*envProbe{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, EnvProbePrefix) || v == "" {
			continue
		}
		rest := k[len(EnvProbePrefix):]
		name, field, found := strings.Cut(rest, "_")
		if !found || name == "" || field == "" {
			return nil, fmt.Errorf("%s: expected %s<NAME>_<FIELD>, for example %sPG_TYPE", k, EnvProbePrefix, EnvProbePrefix)
		}
		if !isEnvProbeName(name) {
			return nil, fmt.Errorf("%s: probe name %q may hold letters and digits only; the first underscore ends the name", k, name)
		}
		segs := strings.Split(strings.ToLower(field), "__")
		for _, s := range segs {
			if s == "" {
				return nil, fmt.Errorf("%s: empty segment in %q; use __ between nested keys and a single _ inside a key name", k, field)
			}
		}
		lname := strings.ToLower(name)
		g := byName[lname]
		if g == nil {
			g = &envProbe{name: lname}
			byName[lname] = g
		}
		g.settings = append(g.settings, envSetting{variable: k, segments: segs, value: v})
	}
	out := make([]envProbe, 0, len(byName))
	for _, g := range byName {
		sort.Slice(g.settings, func(i, j int) bool {
			a, b := g.settings[i], g.settings[j]
			if len(a.segments) != len(b.segments) {
				return len(a.segments) < len(b.segments)
			}
			return a.variable < b.variable
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func isEnvProbeName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// nameKey makes a probe name comparable with an environment name, which
// cannot hold a hyphen or an underscore: smtp-prod and smtp_prod both
// answer to SMTPPROD.
func nameKey(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
}

// touched records one value the environment wrote, by canonical dotted
// path, so a schema problem can be traced back to its variable.
type touched struct {
	path     string
	variable string
}

func mergeEnvProbe(data *LocalConfigurationData, g envProbe, log *logger.ModuleLogger) error {
	prefix := EnvProbePrefix + strings.ToUpper(g.name) + "_"

	idx := -1
	for i, p := range data.Probes {
		if nameKey(p.Name) != g.name {
			continue
		}
		if idx >= 0 {
			return fmt.Errorf("%s*: the name %q matches two probes of the configuration (%q and %q); rename one", prefix, g.name, data.Probes[idx].Name, p.Name)
		}
		idx = i
	}

	var declared *envSetting
	for i := range g.settings {
		if len(g.settings[i].segments) == 1 && g.settings[i].segments[0] == "type" {
			declared = &g.settings[i]
		}
	}

	var probe ProbeConfig
	if idx >= 0 {
		probe = data.Probes[idx]
	} else {
		if declared == nil {
			return fmt.Errorf("%s*: no probe is named %q in the configuration and %sTYPE is not set; set it to create the probe", prefix, g.name, prefix)
		}
		probe = ProbeConfig{Name: g.name}
	}
	if declared != nil {
		t := strings.TrimSpace(declared.value)
		if probe.Type != "" && probe.Type != t {
			return fmt.Errorf("%s=%s: the probe %q of the configuration is of type %q", declared.variable, t, probe.Name, probe.Type)
		}
		probe.Type = t
	}

	sc, hasSpec := spec.For(probe.Type)
	if !hasSpec && probeTypeLookup != nil && !probeTypeLookup(probe.Type) {
		return fmt.Errorf("%s%s: unknown probe type %q (not built into this agent)", prefix, "TYPE", probe.Type)
	}
	if !hasSpec && log != nil {
		log.Warn().Str("probe", probe.Name).Str("type", probe.Type).
			Msg("Probe configured from the environment has no parameter schema: values are read as strings and are not checked")
	}

	params := cloneMap(probe.Params)
	if params == nil {
		params = map[string]interface{}{}
	}
	var paramTouched, govTouched []touched
	seen := map[string]string{} // scope+path -> variable, to refuse two writers of one key
	var governance map[string]interface{}
	if probe.Governance != nil {
		governance = cloneMap(probe.Governance)
	}

	for _, s := range g.settings {
		segs := s.segments
		first := segs[0]
		switch {
		case first == "type" && len(segs) == 1:
			continue
		case first == "params" && len(segs) > 1:
			segs = segs[1:]
		case probeLevelFields[first]:
			if err := applyProbeLevel(&probe, &governance, s, seen); err != nil {
				return err
			}
			if first == "governance" {
				govTouched = append(govTouched, touched{path: strings.Join(segs[1:], "."), variable: s.variable})
			}
			continue
		}
		path, val, err := resolveSetting(sc.Params, hasSpec, segs, s)
		if err != nil {
			return err
		}
		dotted := strings.Join(path, ".")
		if prev, dup := seen["params."+dotted]; dup {
			return fmt.Errorf("%s: sets %q, already set by %s", s.variable, dotted, prev)
		}
		seen["params."+dotted] = s.variable
		setPath(params, path, val)
		paramTouched = append(paramTouched, touched{path: dotted, variable: s.variable})
	}

	if hasSpec {
		if err := checkAgainstSchema(sc, prefix, params, paramTouched); err != nil {
			return err
		}
		if len(govTouched) > 0 {
			for _, p := range spec.CheckGovernance(governance) {
				for _, t := range govTouched {
					if p.Key == "governance."+t.path || strings.HasPrefix(p.Key, "governance."+t.path+".") {
						return fmt.Errorf("%s: %s", t.variable, p)
					}
				}
			}
		}
	}

	probe.Params = params
	probe.Governance = governance
	vars := make([]string, 0, len(g.settings))
	for _, s := range g.settings {
		vars = append(vars, s.variable)
	}
	if idx >= 0 {
		data.Probes[idx] = probe
		if log != nil {
			log.Info().Str("probe", probe.Name).Strs("variables", vars).Msg("Probe of the configuration overridden from the environment")
		}
	} else {
		data.Probes = append(data.Probes, probe)
		if log != nil {
			log.Info().Str("probe", probe.Name).Str("type", probe.Type).Strs("variables", vars).Msg("Probe declared from the environment")
		}
	}
	return nil
}

// applyProbeLevel sets the fields of the probe entry that sit beside
// params: enabled, log_strategies, custom_tags, governance.
func applyProbeLevel(p *ProbeConfig, governance *map[string]interface{}, s envSetting, seen map[string]string) error {
	key := strings.Join(s.segments, ".")
	if prev, dup := seen[key]; dup {
		return fmt.Errorf("%s: sets %q, already set by %s", s.variable, key, prev)
	}
	seen[key] = s.variable

	switch s.segments[0] {
	case "enabled":
		if len(s.segments) > 1 {
			return fmt.Errorf("%s: enabled is a single value", s.variable)
		}
		b, err := parseBool(s.value)
		if err != nil {
			return fmt.Errorf("%s: %w", s.variable, err)
		}
		p.Enabled = &b
	case "log_strategies":
		if len(s.segments) > 1 {
			return fmt.Errorf("%s: log_strategies is a single list", s.variable)
		}
		list, err := parseList(s.value)
		if err != nil {
			return fmt.Errorf("%s: %w", s.variable, err)
		}
		for _, it := range list {
			if !IsLogCapableStrategy(it.(string)) {
				return fmt.Errorf("%s: %q cannot receive logs; accepted: %s", s.variable, it, strings.Join(LogCapableStrategies, ", "))
			}
		}
		p.LogStrategies = toStrings(list)
	case "custom_tags":
		existing := map[string]interface{}{}
		for k, v := range p.CustomTags {
			existing[k] = v
		}
		holder := map[string]interface{}{"custom_tags": existing}
		fields := []spec.ParamSpec{{Key: "custom_tags", Kind: spec.KindMap}}
		path, val, err := resolveSetting(fields, true, s.segments, s)
		if err != nil {
			return err
		}
		setPath(holder, path, val)
		tags := map[string]string{}
		merged, _ := asMap(holder["custom_tags"])
		for k, v := range merged {
			str, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: custom tag %q must be a string", s.variable, k)
			}
			tags[k] = str
		}
		p.CustomTags = tags
	case "governance":
		if len(s.segments) == 1 {
			return fmt.Errorf("%s: name the key (GOVERNANCE__CRITICALITY, GOVERNANCE__LABELS__APPLICATION) or give a JSON object", s.variable)
		}
		if *governance == nil {
			*governance = map[string]interface{}{}
		}
		path, val, err := resolveSetting(spec.GovernanceFields(), true, s.segments[1:], s)
		if err != nil {
			return err
		}
		setPath(*governance, path, val)
	}
	return nil
}

// checkAgainstSchema runs the shared schema check on the merged params and
// reports only what the environment is answerable for: the values it
// wrote, and a required key still absent.
func checkAgainstSchema(sc spec.Probe, prefix string, params map[string]interface{}, ts []touched) error {
	for _, p := range sc.CheckParams(params) {
		if p.Kind == spec.ProblemMissing {
			return fmt.Errorf("%s%s: %s is %s; set it, or declare it in the file of this probe", prefix, envFieldName(p.Key), p.Key, p.Message)
		}
		for _, t := range ts {
			if p.Key == t.path || strings.HasPrefix(p.Key, t.path+".") || strings.HasPrefix(p.Key, t.path+"[") {
				return fmt.Errorf("%s: %s", t.variable, p)
			}
		}
	}
	return nil
}

func envFieldName(dotted string) string {
	return strings.ToUpper(strings.ReplaceAll(dotted, ".", "__"))
}

// resolveSetting maps the segments of one variable to the canonical path
// of a parameter and its typed value. A trailing _FILE reads the value
// from the file the variable names, unless the schema owns a key of that
// very name (tls.ca_file): the exact key wins, and its file form is
// ..._CA_FILE_FILE.
func resolveSetting(specs []spec.ParamSpec, hasSpec bool, segs []string, s envSetting) ([]string, interface{}, error) {
	fromFile := false
	r, err := resolvePath(specs, hasSpec, segs)
	if err != nil {
		last := segs[len(segs)-1]
		if !strings.HasSuffix(last, fileSuffix) || len(last) == len(fileSuffix) {
			return nil, nil, fmt.Errorf("%s: %w", s.variable, err)
		}
		stripped := append(append([]string{}, segs[:len(segs)-1]...), strings.TrimSuffix(last, fileSuffix))
		r2, err2 := resolvePath(specs, hasSpec, stripped)
		if err2 != nil {
			return nil, nil, fmt.Errorf("%s: %w", s.variable, err)
		}
		r, fromFile = r2, true
	} else if !hasSpec && strings.HasSuffix(segs[len(segs)-1], fileSuffix) && len(segs[len(segs)-1]) > len(fileSuffix) {
		stripped := append(append([]string{}, segs[:len(segs)-1]...), strings.TrimSuffix(segs[len(segs)-1], fileSuffix))
		r, _ = resolvePath(specs, hasSpec, stripped)
		fromFile = true
	}

	raw := s.value
	if fromFile {
		path := strings.TrimSpace(s.value)
		b, err := os.ReadFile(path) // #nosec G304 - the operator names the file holding the value
		if err != nil {
			return nil, nil, fmt.Errorf("%s: reading %s: %w", s.variable, path, err)
		}
		raw = strings.TrimSpace(string(b))
		if raw == "" {
			return nil, nil, fmt.Errorf("%s: %s is empty", s.variable, path)
		}
		if r.secret && r.kind == spec.KindString && !strings.ContainsAny(path, "}") {
			return r.path, "${file:" + path + "}", nil
		}
	} else if r.secret && r.kind == spec.KindString {
		// The value stays in the environment: the configuration holds a
		// reference, which `config show --raw` prints in its place and
		// the substitution pass resolves.
		return r.path, "${env:" + s.variable + "}", nil
	}
	val, err := convertValue(r, raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", s.variable, err)
	}
	return r.path, escapeDollars(val), nil
}

type resolvedPath struct {
	path   []string
	kind   spec.ParamKind
	leaf   spec.ParamSpec
	secret bool
}

// resolvePath walks segs down the schema, matching each segment to a key
// case-insensitively (an alternative spelling resolves to the canonical
// key). A map takes the next segment as its entry; a block without declared
// fields takes the rest as free keys.
func resolvePath(specs []spec.ParamSpec, hasSpec bool, segs []string) (resolvedPath, error) {
	if !hasSpec {
		leaf := segs[len(segs)-1]
		return resolvedPath{path: segs, kind: spec.KindString, secret: secret.IsSensitiveKey(leaf)}, nil
	}
	var out resolvedPath
	cur := specs
	free := false
	for i, seg := range segs {
		if free {
			out.path = append(out.path, seg)
			if i == len(segs)-1 {
				out.kind = spec.KindString
				out.secret = out.secret || secret.IsSensitiveKey(seg)
			}
			continue
		}
		p, ok := findSpec(cur, seg)
		if !ok {
			return resolvedPath{}, fmt.Errorf("%q is not a parameter of this probe type%s", strings.Join(append(append([]string{}, out.path...), seg), "."), suggest(cur))
		}
		out.path = append(out.path, p.Key)
		last := i == len(segs)-1
		if last {
			out.kind, out.leaf = p.Kind, p
			out.secret = p.Secret
			return out, nil
		}
		switch p.Kind {
		case spec.KindBlock:
			if len(p.Fields) == 0 {
				free = true
				out.secret = p.Secret
				continue
			}
			cur = p.Fields
		case spec.KindMap:
			if len(segs)-i-1 != 1 {
				return resolvedPath{}, fmt.Errorf("%q is a mapping: name one entry after it, as in %s__KEY", p.Key, strings.ToUpper(p.Key))
			}
			out.path = append(out.path, segs[i+1])
			out.kind = spec.KindString
			out.secret = p.Secret || secret.IsSensitiveKey(segs[i+1])
			return out, nil
		case spec.KindBlockList:
			return resolvedPath{}, fmt.Errorf("%q is a list of blocks: give the whole list as JSON in %s", p.Key, strings.ToUpper(p.Key))
		default:
			return resolvedPath{}, fmt.Errorf("%q is not a block, it takes no nested key", p.Key)
		}
	}
	return out, nil
}

func findSpec(specs []spec.ParamSpec, seg string) (spec.ParamSpec, bool) {
	for _, p := range specs {
		if strings.EqualFold(p.Key, seg) {
			return p, true
		}
		for _, alt := range p.AlsoAccepts {
			if strings.EqualFold(alt, seg) {
				return p, true
			}
		}
	}
	return spec.ParamSpec{}, false
}

func suggest(specs []spec.ParamSpec) string {
	keys := make([]string, 0, len(specs))
	for _, p := range specs {
		keys = append(keys, p.Key)
	}
	if len(keys) == 0 {
		return ""
	}
	return "; known here: " + strings.Join(keys, ", ")
}

// convertValue types a string by the kind the schema declares.
func convertValue(r resolvedPath, raw string) (interface{}, error) {
	switch r.kind {
	case spec.KindString, "":
		return raw, nil
	case spec.KindInt:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", raw)
		}
		return int(n), nil
	case spec.KindFloat:
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return f, nil
	case spec.KindBool:
		return parseBool(raw)
	case spec.KindDuration:
		v := strings.TrimSpace(raw)
		if n, err := strconv.Atoi(v); err == nil {
			return n, nil
		}
		if _, err := time.ParseDuration(v); err != nil {
			return nil, fmt.Errorf("%q is neither seconds nor a duration such as 30s or 5m", raw)
		}
		return v, nil
	case spec.KindStringList:
		list, err := parseList(raw)
		if err != nil {
			return nil, err
		}
		return list, nil
	case spec.KindMap, spec.KindBlockList, spec.KindBlock:
		v := strings.TrimSpace(raw)
		if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") {
			parsed, err := parseJSON(v)
			if err != nil {
				return nil, fmt.Errorf("not valid JSON: %w", err)
			}
			_, isMap := parsed.(map[string]interface{})
			_, isList := parsed.([]interface{})
			if r.kind == spec.KindBlockList && !isList {
				return nil, fmt.Errorf("expected a JSON array")
			}
			if r.kind != spec.KindBlockList && !isMap {
				return nil, fmt.Errorf("expected a JSON object")
			}
			return parsed, nil
		}
		if r.kind == spec.KindBlock {
			if b, err := parseBool(v); err == nil {
				return b, nil
			}
			if r.leaf.ScalarMeans != "" {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("this parameter holds a %s: give it as JSON, or set its entries one by one with __", r.kind)
	}
	return raw, nil
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%q is not true or false", s)
}

// parseList reads a comma-separated list, or a JSON array of strings for
// the values that hold a comma themselves.
func parseList(s string) ([]interface{}, error) {
	v := strings.TrimSpace(s)
	if strings.HasPrefix(v, "[") {
		parsed, err := parseJSON(v)
		if err != nil {
			return nil, fmt.Errorf("not valid JSON: %w", err)
		}
		items, _ := parsed.([]interface{})
		for _, it := range items {
			if _, ok := it.(string); !ok {
				return nil, fmt.Errorf("list items must be strings")
			}
		}
		return items, nil
	}
	var out []interface{}
	for _, part := range strings.Split(v, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the list is empty")
	}
	return out, nil
}

func toStrings(in []interface{}) []string {
	out := make([]string, 0, len(in))
	for _, it := range in {
		out = append(out, it.(string))
	}
	return out
}

// parseJSON decodes with whole numbers kept whole, the way the YAML
// loader types them.
func parseJSON(s string) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing content after the value")
	}
	return normalizeJSONNumbers(v), nil
}

func normalizeJSONNumbers(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n)
		}
		f, _ := t.Float64()
		return f
	case map[string]interface{}:
		for k, val := range t {
			t[k] = normalizeJSONNumbers(val)
		}
	case []interface{}:
		for i, val := range t {
			t[i] = normalizeJSONNumbers(val)
		}
	}
	return v
}

// escapeDollars doubles the $ of every string, so a value read from the
// environment passes the substitution pass as written.
func escapeDollars(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, "$", "$$")
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, it := range t {
			out[i] = escapeDollars(it)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, it := range t {
			out[k] = escapeDollars(it)
		}
		return out
	}
	return v
}

// setPath writes val at path, creating the blocks on the way. A block meeting
// a block is merged key by key; anything else is replaced.
func setPath(dst map[string]interface{}, path []string, val interface{}) {
	for _, k := range path[:len(path)-1] {
		next, ok := asMap(dst[k])
		if !ok {
			next = map[string]interface{}{}
		}
		dst[k] = next
		dst = next
	}
	last := path[len(path)-1]
	if incoming, ok := val.(map[string]interface{}); ok {
		if existing, ok := asMap(dst[last]); ok {
			for k, v := range incoming {
				existing[k] = v
			}
			dst[last] = existing
			return
		}
	}
	dst[last] = val
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	}
	return nil, false
}

// cloneMap copies the nested maps, so the environment never writes into
// the structure a caller still holds.
func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if sub, ok := asMap(v); ok {
			out[k] = cloneMap(sub)
		} else {
			out[k] = v
		}
	}
	return out
}

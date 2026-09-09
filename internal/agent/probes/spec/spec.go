package spec

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ParamKind is the widened type a form control, a validator and a doc
// table all key off. It mirrors the helpers in types/params.go: a
// duration accepts a bare number of seconds or a Go duration string.
type ParamKind string

const (
	KindString     ParamKind = "string"
	KindInt        ParamKind = "int"
	KindFloat      ParamKind = "float"
	KindBool       ParamKind = "bool"
	KindDuration   ParamKind = "duration"
	KindStringList ParamKind = "string_list"
	KindBlock      ParamKind = "block"
	// KindMap is a free-form mapping of string keys to string values
	// (environment variables for exec); its keys are the operator's, so
	// none are declared and none are reported as unknown.
	KindMap ParamKind = "map"
	// KindBlockList is a list of mappings, each checked against Fields
	// (SNMP custom mappings, v3 users, governance rules).
	KindBlockList ParamKind = "block_list"
)

// ParamSpec describes one key an operator may write under a probe's
// params block. Secret fields are stored in the secret store by the
// configurator and referenced from the fragment, never written in clear.
type ParamSpec struct {
	Key         string      `json:"key"`
	Kind        ParamKind   `json:"kind"`
	Required    bool        `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Secret      bool        `json:"secret,omitempty"`
	Description string      `json:"description,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Group       string      `json:"group,omitempty"`
	Example     string      `json:"example,omitempty"`
	Fields      []ParamSpec `json:"fields,omitempty"`
	// AlsoAccepts lists alternative spellings the parser reads for the
	// same meaning (tls.ca_cert for ca_file), so the guard test does not
	// flag them and the configurator writes the canonical one.
	AlsoAccepts []string `json:"also_accepts,omitempty"`
	// Essential marks a parameter the probe does nothing useful without,
	// even though the parser accepts its absence (a database user, an
	// SNMP community). The console asks for it before anything optional;
	// Required stays the parser's word and is checked, Essential is not.
	Essential bool `json:"essential,omitempty"`
	// Advanced marks a field of a block that a form folds away until the
	// operator asks for it: an override few configurations set (the
	// per-signal transport of an OTLP output). It changes nothing for
	// the parser or the checks.
	Advanced bool `json:"advanced,omitempty"`
	// EssentialWhen makes the parameter essential only while every
	// listed condition holds against the current values (the v3 block
	// of snmp_poll when version is v3). A condition reads the sibling
	// key's value, or its default when unset.
	EssentialWhen []Condition `json:"essential_when,omitempty"`
}

// Condition is one "sibling key has one of these values" test.
type Condition struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// HasStartSet reports whether at least one top-level parameter is
// Required, Essential or conditionally essential: what the console puts
// in front of the operator before the optional settings.
func (s Probe) HasStartSet() bool {
	for _, p := range s.Params {
		if p.Required || p.Essential || len(p.EssentialWhen) > 0 {
			return true
		}
	}
	return false
}

// Probe is what the configurator, config check and the docs need
// about a probe type that its constructor alone cannot tell them.
type Probe struct {
	Type            string `json:"type"`
	DisplayName     string `json:"display_name"`
	Category        string `json:"category,omitempty"`
	Summary         string `json:"summary,omitempty"`
	DocsPath        string `json:"docs_path,omitempty"`
	MultiInstance   bool   `json:"multi_instance"`
	DefaultInterval int    `json:"default_interval,omitempty"`
	// Platforms lists the operating systems (GOOS names) the probe can
	// run on; empty means every platform. A probe registers everywhere
	// through a stub so one configuration serves a mixed fleet, and the
	// catalogue uses this to say why it will not start here.
	Platforms []string    `json:"platforms,omitempty"`
	Params    []ParamSpec `json:"params"`
}

// RunsOn reports whether the probe can run on the given GOOS.
func (s Probe) RunsOn(goos string) bool {
	if len(s.Platforms) == 0 {
		return true
	}
	for _, p := range s.Platforms {
		if p == goos {
			return true
		}
	}
	return false
}

var (
	specMu sync.RWMutex
	specs  = map[string]Probe{}
)

// RegisterProbe declares a probe's parameter schema. Called from the
// probe package's init(), next to RegisterProbe and RegisterLegacyParams,
// so the description lives beside the parser it describes. A duplicate
// or nameless registration is a programming error, like RegisterProbe.
func Register(spec Probe) {
	if spec.Type == "" {
		panic("probes: RegisterProbe with an empty Type")
	}
	specMu.Lock()
	defer specMu.Unlock()
	if _, dup := specs[spec.Type]; dup {
		panic(fmt.Sprintf("spec: %q registered twice", spec.Type))
	}
	specs[spec.Type] = spec
}

// unregisterProbe removes a declaration. Tests only: production code
// declares once, at init, and never takes a schema back.
func unregister(probeType string) {
	specMu.Lock()
	defer specMu.Unlock()
	delete(specs, probeType)
}

// ProbeFor returns the schema of a probe type, if one is declared.
func For(probeType string) (Probe, bool) {
	specMu.RLock()
	defer specMu.RUnlock()
	s, ok := specs[probeType]
	return s, ok
}

// RegisteredProbes returns every declared schema, sorted by type.
func Registered() []Probe {
	specMu.RLock()
	defer specMu.RUnlock()
	out := make([]Probe, 0, len(specs))
	for _, s := range specs {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// DeclaredKeys returns the flattened set of keys a spec declares, with
// nested block fields as "block.field" and alternative spellings
// included, so a static guard can compare it to what the parser reads.
func (s Probe) DeclaredKeys() map[string]struct{} {
	out := map[string]struct{}{}
	var walk func(prefix string, params []ParamSpec)
	walk = func(prefix string, params []ParamSpec) {
		for _, p := range params {
			out[prefix+p.Key] = struct{}{}
			for _, alt := range p.AlsoAccepts {
				out[prefix+alt] = struct{}{}
			}
			if p.Kind == KindBlock || p.Kind == KindBlockList {
				walk(prefix+p.Key+".", p.Fields)
			}
		}
	}
	walk("", s.Params)
	return out
}

// ProblemKind says what a SpecProblem is about, so a consumer can decide
// which problems it owns: config check reports missing and unknown keys
// from the spec and leaves shapes to the constructor, which already
// reports what it could not read.
type ProblemKind string

const (
	ProblemMissing ProblemKind = "missing"
	ProblemUnknown ProblemKind = "unknown"
	ProblemShape   ProblemKind = "shape"
)

// SpecProblem is one thing wrong with a params map against its spec.
type SpecProblem struct {
	Key     string
	Kind    ProblemKind
	Message string
}

func (p SpecProblem) String() string { return p.Key + ": " + p.Message }

// CheckParams validates a params map against the spec: missing required
// keys, values outside an enum, values of the wrong shape, and keys the
// spec does not know. It is the shared front door for config check, the
// web configurator and the validation endpoint; the constructor remains
// the last word on what the probe can actually use.
func (s Probe) CheckParams(params map[string]interface{}) []SpecProblem {
	var problems []SpecProblem
	checkBlock("", s.Params, params, &problems)
	return problems
}

func checkBlock(prefix string, specs []ParamSpec, params map[string]interface{}, out *[]SpecProblem) {
	known := map[string]ParamSpec{}
	for _, p := range specs {
		known[p.Key] = p
		for _, alt := range p.AlsoAccepts {
			known[alt] = p
		}
	}
	for _, p := range specs {
		if !p.Required {
			continue
		}
		if _, ok := params[p.Key]; ok {
			continue
		}
		present := false
		for _, alt := range p.AlsoAccepts {
			if _, ok := params[alt]; ok {
				present = true
				break
			}
		}
		if !present {
			*out = append(*out, SpecProblem{Key: prefix + p.Key, Kind: ProblemMissing, Message: "required"})
		}
	}
	for key, value := range params {
		p, ok := known[key]
		if !ok {
			*out = append(*out, SpecProblem{Key: prefix + key, Kind: ProblemUnknown, Message: "not a parameter of this probe"})
			continue
		}
		if msg := checkKind(p, value); msg != "" {
			*out = append(*out, SpecProblem{Key: prefix + key, Kind: ProblemShape, Message: msg})
			continue
		}
		switch p.Kind {
		case KindBlock:
			if nested, ok := asStringMap(value); ok {
				checkBlock(prefix+key+".", p.Fields, nested, out)
			}
		case KindBlockList:
			if items, ok := value.([]interface{}); ok {
				for i, item := range items {
					if nested, ok := asStringMap(item); ok {
						checkBlock(fmt.Sprintf("%s%s[%d].", prefix, key, i), p.Fields, nested, out)
					}
				}
			}
		}
	}
}

func checkKind(p ParamSpec, v interface{}) string {
	switch p.Kind {
	case KindString:
		s, ok := v.(string)
		if !ok {
			return fmt.Sprintf("must be a string, got %T", v)
		}
		if len(p.Enum) > 0 && !contains(p.Enum, s) {
			return fmt.Sprintf("must be one of %v", p.Enum)
		}
	case KindMap:
		m, ok := asStringMap(v)
		if !ok {
			return fmt.Sprintf("must be a mapping of names to values, got %T", v)
		}
		for k, val := range m {
			if _, isStr := val.(string); !isStr {
				return fmt.Sprintf("value of %q must be a string, got %T", k, val)
			}
		}
	case KindInt:
		switch n := v.(type) {
		case int, int64:
		case float64:
			if n != float64(int64(n)) {
				return "must be a whole number"
			}
		default:
			return fmt.Sprintf("must be a number, got %T", v)
		}
	case KindFloat:
		switch v.(type) {
		case int, int64, float64:
		default:
			return fmt.Sprintf("must be a number, got %T", v)
		}
	case KindBool:
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("must be true or false, got %T", v)
		}
	case KindDuration:
		switch v.(type) {
		case int, int64, float64, string:
		default:
			return fmt.Sprintf("must be seconds or a duration like 30s, got %T", v)
		}
	case KindStringList:
		items, ok := v.([]interface{})
		if !ok {
			if _, single := v.(string); single {
				return ""
			}
			return fmt.Sprintf("must be a list of strings, got %T", v)
		}
		for _, it := range items {
			s, ok := it.(string)
			if !ok {
				return fmt.Sprintf("list items must be strings, got %T", it)
			}
			if len(p.Enum) > 0 && !contains(p.Enum, s) {
				return fmt.Sprintf("items must be one of %v", p.Enum)
			}
		}
	case KindBlockList:
		items, ok := v.([]interface{})
		if !ok {
			return fmt.Sprintf("must be a list of blocks, got %T", v)
		}
		for _, it := range items {
			if _, ok := asStringMap(it); !ok {
				return fmt.Sprintf("list items must be blocks of settings, got %T", it)
			}
		}
	case KindBlock:
		if _, ok := asStringMap(v); !ok {
			// A few parsers accept a bare bool for a block (mysql tls:
			// true); the parser decides, the spec only refuses nonsense.
			if _, isBool := v.(bool); !isBool {
				return fmt.Sprintf("must be a block of settings, got %T", v)
			}
		}
	}
	return ""
}

// asStringMap accepts both the yaml.v2 and yaml.v3 shapes of a nested
// mapping, which reach here depending on the loader that produced them.
func asStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}

// contains is case-insensitive: the parsers that use closed sets (event
// levels, ssl modes) fold case, and the spec must not reject a value the
// probe accepts.
func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

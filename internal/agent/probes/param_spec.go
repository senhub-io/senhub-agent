package probes

import (
	"fmt"
	"sort"
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
)

// ParamSpec describes one key an operator may write under a probe's
// params block. Secret fields are stored in the secret store by the
// configurator and referenced from the fragment, never written in clear.
type ParamSpec struct {
	Key         string
	Kind        ParamKind
	Required    bool
	Default     interface{}
	Secret      bool
	Description string
	Enum        []string
	Group       string
	Example     string
	Fields      []ParamSpec
	// AlsoAccepts lists alternative spellings the parser reads for the
	// same meaning (tls.ca_cert for ca_file), so the guard test does not
	// flag them and the configurator writes the canonical one.
	AlsoAccepts []string
}

// ProbeSpec is what the configurator, config check and the docs need
// about a probe type that its constructor alone cannot tell them.
type ProbeSpec struct {
	Type            string
	DisplayName     string
	Category        string
	Summary         string
	DocsPath        string
	MultiInstance   bool
	DefaultInterval int
	Params          []ParamSpec
}

var (
	specMu sync.RWMutex
	specs  = map[string]ProbeSpec{}
)

// RegisterProbeSpec declares a probe's parameter schema. Called from the
// probe package's init(), next to RegisterProbe and RegisterLegacyParams,
// so the description lives beside the parser it describes. A duplicate
// or nameless registration is a programming error, like RegisterProbe.
func RegisterProbeSpec(spec ProbeSpec) {
	if spec.Type == "" {
		panic("probes: RegisterProbeSpec with an empty Type")
	}
	specMu.Lock()
	defer specMu.Unlock()
	if _, dup := specs[spec.Type]; dup {
		panic(fmt.Sprintf("probes: spec for %q registered twice", spec.Type))
	}
	specs[spec.Type] = spec
}

// ProbeSpecFor returns the schema of a probe type, if one is declared.
func ProbeSpecFor(probeType string) (ProbeSpec, bool) {
	specMu.RLock()
	defer specMu.RUnlock()
	s, ok := specs[probeType]
	return s, ok
}

// RegisteredProbeSpecs returns every declared schema, sorted by type.
func RegisteredProbeSpecs() []ProbeSpec {
	specMu.RLock()
	defer specMu.RUnlock()
	out := make([]ProbeSpec, 0, len(specs))
	for _, s := range specs {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// DeclaredKeys returns the flattened set of keys a spec declares, with
// nested block fields as "block.field" and alternative spellings
// included, so a static guard can compare it to what the parser reads.
func (s ProbeSpec) DeclaredKeys() map[string]struct{} {
	out := map[string]struct{}{}
	var walk func(prefix string, params []ParamSpec)
	walk = func(prefix string, params []ParamSpec) {
		for _, p := range params {
			out[prefix+p.Key] = struct{}{}
			for _, alt := range p.AlsoAccepts {
				out[prefix+alt] = struct{}{}
			}
			if p.Kind == KindBlock {
				walk(prefix+p.Key+".", p.Fields)
			}
		}
	}
	walk("", s.Params)
	return out
}

// SpecProblem is one thing wrong with a params map against its spec.
type SpecProblem struct {
	Key     string
	Message string
}

func (p SpecProblem) String() string { return p.Key + ": " + p.Message }

// CheckParams validates a params map against the spec: missing required
// keys, values outside an enum, values of the wrong shape, and keys the
// spec does not know. It is the shared front door for config check, the
// web configurator and the validation endpoint; the constructor remains
// the last word on what the probe can actually use.
func (s ProbeSpec) CheckParams(params map[string]interface{}) []SpecProblem {
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
			*out = append(*out, SpecProblem{Key: prefix + p.Key, Message: "required"})
		}
	}
	for key, value := range params {
		p, ok := known[key]
		if !ok {
			*out = append(*out, SpecProblem{Key: prefix + key, Message: "not a parameter of this probe"})
			continue
		}
		if msg := checkKind(p, value); msg != "" {
			*out = append(*out, SpecProblem{Key: prefix + key, Message: msg})
			continue
		}
		if p.Kind == KindBlock {
			if nested, ok := asStringMap(value); ok {
				checkBlock(prefix+key+".", p.Fields, nested, out)
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

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

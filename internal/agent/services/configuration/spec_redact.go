package configuration

import (
	"fmt"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

// maskSpecSecrets masks, in a configuration about to be shown or sent
// off the host, every parameter that its probe or output spec declares
// secret, whatever the parameter is called.
func maskSpecSecrets(data *LocalConfigurationData) {
	for _, p := range data.Probes {
		if s, ok := spec.For(p.Type); ok {
			maskSecretPaths(p.Params, s)
		}
	}
	for _, st := range data.Storage {
		if o, ok := outputspec.For(st.Name); ok {
			maskSecretPaths(st.Params, spec.Probe{Type: o.Type, Params: o.Params})
		}
	}
}

// maskSecretPaths masks every parameter the schema declares secret,
// sparing the keys a secret mapping declares readable.
func maskSecretPaths(params interface{}, s spec.Probe) {
	for _, path := range s.SecretPaths() {
		maskSpecPathKeeping(params, strings.Split(path, "."), s.ReadableKeysAt(path))
	}
}

// maskSpecPath masks the value at the dotted path segs inside v.
// Anything that is not a map is left untouched.
func maskSpecPath(v interface{}, segs []string) { maskSpecPathKeeping(v, segs, nil) }

// maskSpecPathKeeping is maskSpecPath for a secret mapping that declares
// some keys readable: those entries (compared without case) keep their
// value, every other entry is masked.
func maskSpecPathKeeping(v interface{}, segs []string, keep []string) {
	switch m := v.(type) {
	case map[string]interface{}:
		maskSpecPathMap(m, segs, keep)
	case map[interface{}]interface{}:
		maskSpecPathIfaceMap(m, segs, keep)
	}
}

func keptKey(k interface{}, keep []string) bool {
	name := fmt.Sprint(k)
	for _, w := range keep {
		if strings.EqualFold(name, w) {
			return true
		}
	}
	return false
}

func maskSpecPathMap(m map[string]interface{}, segs []string, keep []string) {
	if len(segs) == 0 {
		return
	}
	if len(segs) == 1 {
		val, ok := m[segs[0]]
		if !ok || val == nil {
			return
		}
		switch inner := val.(type) {
		case map[string]interface{}:
			for k := range inner {
				if !keptKey(k, keep) {
					inner[k] = "***"
				}
			}
		case map[interface{}]interface{}:
			for k := range inner {
				if !keptKey(k, keep) {
					inner[k] = "***"
				}
			}
		default:
			m[segs[0]] = "***"
		}
		return
	}
	child, ok := m[segs[0]]
	if !ok || child == nil {
		return
	}
	switch c := child.(type) {
	case map[string]interface{}:
		maskSpecPathMap(c, segs[1:], keep)
	case map[interface{}]interface{}:
		maskSpecPathIfaceMap(c, segs[1:], keep)
	case []interface{}:
		for _, el := range c {
			maskSpecPathKeeping(el, segs[1:], keep)
		}
	}
}

// maskSpecPathIfaceMap handles maps produced by a YAML decoder as
// map[interface{}]interface{}: keys are matched by fmt.Sprint(key) and
// written back with the original key.
func maskSpecPathIfaceMap(m map[interface{}]interface{}, segs []string, keep []string) {
	if len(segs) == 0 {
		return
	}
	var key interface{}
	found := false
	for k := range m {
		if fmt.Sprint(k) == segs[0] {
			key = k
			found = true
			break
		}
	}
	if !found {
		return
	}
	if len(segs) == 1 {
		val := m[key]
		if val == nil {
			return
		}
		switch inner := val.(type) {
		case map[string]interface{}:
			for k := range inner {
				if !keptKey(k, keep) {
					inner[k] = "***"
				}
			}
		case map[interface{}]interface{}:
			for k := range inner {
				if !keptKey(k, keep) {
					inner[k] = "***"
				}
			}
		default:
			m[key] = "***"
		}
		return
	}
	child := m[key]
	switch c := child.(type) {
	case map[string]interface{}:
		maskSpecPathMap(c, segs[1:], keep)
	case map[interface{}]interface{}:
		maskSpecPathIfaceMap(c, segs[1:], keep)
	case []interface{}:
		for _, el := range c {
			maskSpecPathKeeping(el, segs[1:], keep)
		}
	}
}

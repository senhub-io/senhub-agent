package configuration

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

// The console never sees a stored secret: the listing shows a reference
// and the form sends nothing back for it. A rewrite from the form must
// therefore carry the references the file already holds, or every save
// would drop the credentials that took effort to get right.

// KeepStoredReferences copies into incoming every string leaf of existing
// that is a ${...} reference and that incoming does not set at the same
// path. A path incoming sets, to anything, wins.
func KeepStoredReferences(existing, incoming map[string]interface{}) map[string]interface{} {
	if incoming == nil {
		incoming = map[string]interface{}{}
	}
	for k, v := range existing {
		switch val := v.(type) {
		case map[string]interface{}:
			sub, has := incoming[k].(map[string]interface{})
			if !has {
				if _, set := incoming[k]; set {
					continue
				}
				merged := KeepStoredReferences(val, map[string]interface{}{})
				if len(merged) > 0 {
					incoming[k] = merged
				}
				continue
			}
			KeepStoredReferences(val, sub)
		case []interface{}:
			// A list of blocks is merged item by item, in order: the form
			// re-sends every row, a stored value inside one row is kept.
			list, has := incoming[k].([]interface{})
			if !has {
				continue
			}
			for i, item := range val {
				em, ok := item.(map[string]interface{})
				if !ok || i >= len(list) {
					continue
				}
				if im, ok := list[i].(map[string]interface{}); ok {
					KeepStoredReferences(em, im)
				}
			}
		case string:
			if _, set := incoming[k]; set {
				continue
			}
			if strings.HasPrefix(val, "${") {
				incoming[k] = val
			}
		}
	}
	return incoming
}

// ReadProbeFragment returns a managed probe fragment as written,
// references included, or false when there is no such file.
func ReadProbeFragment(configPath, name string) (ProbeConfig, bool, error) {
	path := ProbeFragmentPath(configPath, name)
	raw, err := os.ReadFile(path) // #nosec G304 - path is under probes.d/
	if err != nil {
		if os.IsNotExist(err) {
			return ProbeConfig{}, false, nil
		}
		return ProbeConfig{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	var list []ProbeConfig
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return ProbeConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, p := range list {
		if p.Name == name {
			p.Params = convertMapTypes(map[string]interface{}(p.Params)).(map[string]interface{})
			return p, true, nil
		}
	}
	return ProbeConfig{}, false, nil
}

// DropNilValues removes, in place, every nil leaf and every mapping left
// empty by it. A form says "remove this stored value" by sending null
// for it: the merge above keeps the key as set, so the reference is not
// carried over, and this takes the null out before the file is written.
func DropNilValues(params map[string]interface{}) {
	for k, v := range params {
		switch val := v.(type) {
		case nil:
			delete(params, k)
		case map[string]interface{}:
			DropNilValues(val)
			if len(val) == 0 {
				delete(params, k)
			}
		}
	}
}

// ReadProbeFragmentParams returns the params of a managed probe fragment
// as written, references included, or nil when there is no such file.
func ReadProbeFragmentParams(configPath, name string) (map[string]interface{}, error) {
	p, found, err := ReadProbeFragment(configPath, name)
	if err != nil || !found {
		return nil, err
	}
	return p.Params, nil
}

// StrategyFragmentParams returns the params of an output's file as
// written, references included, or nil when there is none.
func StrategyFragmentParams(configPath, name string) (map[string]interface{}, error) {
	frags, err := ListStrategyFragments(configPath)
	if err != nil {
		return nil, err
	}
	for _, f := range frags {
		if f.Name == name {
			return f.Params, nil
		}
	}
	return nil, nil
}

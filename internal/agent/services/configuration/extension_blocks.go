package configuration

import "senhub-agent.go/internal/agent/services/extension"

// claimedBlocks keeps the top-level blocks a registered extension claims,
// with their nested maps keyed by string, and nil when none is left.
func claimedBlocks(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	claimed := extension.ConfigBlocks()
	var out map[string]interface{}
	for name, v := range in {
		if !claimed[name] {
			continue
		}
		if out == nil {
			out = make(map[string]interface{})
		}
		out[name] = convertMapTypes(v)
	}
	return out
}

// ExtensionBlock returns a deep copy of the named extension block of the
// loaded configuration.
func (lc *LocalConfiguration) ExtensionBlock(name string) (map[string]interface{}, bool) {
	d := lc.snapshot()
	if d == nil {
		return nil, false
	}
	return ExtensionBlockOf(*d, name)
}

// ExtensionBlockOf returns a deep copy of the named extension block of
// data. A block written as something other than a mapping is reported
// absent.
func ExtensionBlockOf(data LocalConfigurationData, name string) (map[string]interface{}, bool) {
	v, ok := data.Extensions[name]
	if !ok {
		return nil, false
	}
	block, ok := deepCopyValue(v).(map[string]interface{})
	if !ok {
		return nil, false
	}
	return block, true
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, e := range t {
			out[k] = deepCopyValue(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}

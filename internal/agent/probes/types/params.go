package types

// Helpers for reading numeric parameters out of the free-form
// map[string]interface{} that every probe receives as its raw
// configuration.
//
// The free-form map crosses two type-system boundaries before it
// reaches a probe constructor:
//
//  1. yaml.v2 decodes integer literals (`port: 514`) into `int`.
//  2. encoding/json (and any future yaml.v3 swap that goes through
//     `interface{}`) decodes the same literal into `float64`.
//
// Probes that match only one of those concrete types silently fall
// back to their default when the literal arrives in the other shape
// — exactly the foot-gun tracked by senhub-io/senhubagent#136. The
// helpers in this file accept every common numeric encoding so that
// a config of `port: 5140` works the same way regardless of which
// loader populated the map.

import "strconv"

// IntParam reads an integer parameter from a probe config map. It
// accepts int / int32 / int64 / float32 / float64 / numeric string —
// all the shapes that legitimately appear after a YAML or JSON
// decode of an integer literal. Returns ok=false only when the key
// is absent OR holds a value that cannot be interpreted as an
// integer; in particular a float with a non-zero fractional part is
// rejected so a typo like `port: 5140.5` does not silently become
// 5140.
func IntParam(m map[string]interface{}, key string) (int, bool) {
	raw, present := m[key]
	if !present {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		if float32(int(v)) != v {
			return 0, false
		}
		return int(v), true
	case float64:
		if float64(int(v)) != v {
			return 0, false
		}
		return int(v), true
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// FloatParam reads a floating-point parameter from a probe config
// map. Same shape tolerance as IntParam — accepts int and string
// representations alongside the native float types so an operator
// can write `backoff_factor: 2` (int literal) without breaking a
// field declared as float64.
func FloatParam(m map[string]interface{}, key string) (float64, bool) {
	raw, present := m[key]
	if !present {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

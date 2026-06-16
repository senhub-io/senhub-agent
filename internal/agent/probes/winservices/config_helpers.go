package winservices

import (
	"fmt"
	"time"
)

// stringSlice coerces a YAML scalar or list into a []string, dropping empty
// entries. Accepts []interface{}, []string and a bare string.
func stringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}

// durationSeconds parses an interval expressed either as an integer number of
// seconds (the YAML convention for the active-check probes) or as a Go
// duration string ("45s", "2m"). ok is false when the key is absent; an
// invalid value returns an error.
func durationSeconds(v interface{}) (d time.Duration, ok bool, err error) {
	switch t := v.(type) {
	case nil:
		return 0, false, nil
	case int:
		if t <= 0 {
			return 0, false, nil
		}
		return time.Duration(t) * time.Second, true, nil
	case int64:
		if t <= 0 {
			return 0, false, nil
		}
		return time.Duration(t) * time.Second, true, nil
	case float64:
		if t <= 0 {
			return 0, false, nil
		}
		return time.Duration(t * float64(time.Second)), true, nil
	case string:
		if t == "" {
			return 0, false, nil
		}
		parsed, perr := time.ParseDuration(t)
		if perr != nil {
			return 0, false, fmt.Errorf("invalid duration %q: %w", t, perr)
		}
		if parsed <= 0 {
			return 0, false, nil
		}
		return parsed, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported interval type %T", v)
	}
}

package windowseventlog

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// stringSlice coerces a YAML-decoded value into []string, dropping empty
// entries. Accepts the []interface{} the YAML loader produces as well as
// a native []string (test convenience).
func stringSlice(v interface{}) []string {
	var out []string
	switch raw := v.(type) {
	case []interface{}:
		for _, e := range raw {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range raw {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// intSlice coerces a YAML-decoded value into []int. YAML numbers decode
// as float64 (via the JSON-ish path) or int depending on the loader; we
// accept both, plus numeric strings, and reject anything else with an
// error so a typo in the config surfaces at construction time.
func intSlice(v interface{}) ([]int, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected a list, got %T", v)
	}
	out := make([]int, 0, len(raw))
	for _, e := range raw {
		n, err := toInt(e)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func toInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("not an integer: %q", n)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("not an integer: %T", v)
	}
}

// durationParam parses a poll_interval value. Accepts a Go duration
// string ("30s", "5m") or a bare number interpreted as seconds. Returns
// ok=false when the key is absent so the caller keeps its default.
func durationParam(v interface{}) (time.Duration, bool, error) {
	switch d := v.(type) {
	case nil:
		return 0, false, nil
	case string:
		parsed, err := time.ParseDuration(d)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q", d)
		}
		return parsed, true, nil
	case int:
		return time.Duration(d) * time.Second, true, nil
	case int64:
		return time.Duration(d) * time.Second, true, nil
	case float64:
		return time.Duration(d * float64(time.Second)), true, nil
	default:
		return 0, false, fmt.Errorf("invalid duration type %T", v)
	}
}

// levelTextToInt maps the operator-facing level label to the Windows
// Event Log numeric level. Case-insensitive. Returns ok=false for
// unknown labels so parseConfig can reject the config.
func levelTextToInt(text string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "critical":
		return 1, true
	case "error":
		return 2, true
	case "warning":
		return 3, true
	case "information", "info":
		return 4, true
	case "verbose":
		return 5, true
	default:
		return 0, false
	}
}

// buildXPathQuery assembles the wevtapi XPath query string used to
// pre-filter events at the source (the wevtapi engine evaluates it,
// avoiding rendering events we'd discard). It encodes the level filter
// only; EventID include/exclude and provider globs are applied in the
// in-process filter because XPath cannot express glob matching and the
// include/exclude interaction is clearer in Go.
//
// The query targets a single channel — EvtSubscribe takes one channel
// per subscription, so the probe opens one subscription per configured
// channel and passes that channel's query here.
//
// Returned form (no level filter):  *
// With levels [Critical, Error]:    *[System[(Level=1 or Level=2)]]
//
// Pulled out as a pure function so it is unit-testable without wevtapi.
func buildXPathQuery(levelInts []int) string {
	if len(levelInts) == 0 {
		return "*"
	}
	clauses := make([]string, 0, len(levelInts))
	for _, l := range levelInts {
		clauses = append(clauses, "Level="+strconv.Itoa(l))
	}
	return "*[System[(" + strings.Join(clauses, " or ") + ")]]"
}

// truncate caps a string for log-line previews so an unparseable event
// XML blob does not flood the log.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sortStrings is a tiny wrapper kept local so event_xml.go does not need
// to import sort directly (keeps that file's import set focused on the
// XML/time concerns).
func sortStrings(s []string) { sort.Strings(s) }

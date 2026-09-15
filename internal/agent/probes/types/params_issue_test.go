package types

import (
	"testing"
	"time"
)

// TestCollectParamIssues_MalformedIsNotAbsent pins the distinction the
// helpers used to lose: a key holding something unreadable produced the
// same (zero, false) as a key nobody wrote, so every caller's
// `if v, ok := IntParam(...)` fell back to its default in silence and
// the operator's value vanished without a word (#847).
func TestCollectParamIssues_MalformedIsNotAbsent(t *testing.T) {
	config := map[string]interface{}{
		"priority": "pas-un-nombre",
		"timeout":  "quarante-cinq",
		"factor":   []string{"nope"},
		"enabled":  "peut-etre",
		"name":     42,
		"units":    12,
	}

	var got []ParamIssue
	issues := CollectParamIssues(func() {
		IntParam(config, "priority")
		DurationParam(config, "timeout")
		FloatParam(config, "factor")
		BoolParam(config, "enabled")
		StringParam(config, "name")
		StringSliceParam(config, "units")

		// Absent keys are not issues: nothing was written, so nothing
		// was discarded.
		IntParam(config, "absent")
		DurationParam(config, "absent")
		BoolParam(config, "absent")
		StringParam(config, "absent")
		StringSliceParam(config, "absent")
	})
	got = issues

	if len(got) != 6 {
		t.Fatalf("want one issue per unreadable key and none for the absent ones, got %d: %+v", len(got), got)
	}

	seen := map[string]ParamIssue{}
	for _, issue := range got {
		seen[issue.Key] = issue
	}
	for _, key := range []string{"priority", "timeout", "factor", "enabled", "name", "units"} {
		issue, ok := seen[key]
		if !ok {
			t.Errorf("no issue reported for %q", key)
			continue
		}
		if issue.Want == "" {
			t.Errorf("issue for %q says nothing about what was expected", key)
		}
		if issue.Got == nil {
			t.Errorf("issue for %q does not carry the value that was written", key)
		}
	}
}

// TestCollectParamIssues_ReadableValuesAreSilent keeps the shape
// tolerance the helpers were written for: a duration as a bare number,
// a number as a string, a bool from an env substitution.
func TestCollectParamIssues_ReadableValuesAreSilent(t *testing.T) {
	config := map[string]interface{}{
		"priority": "6",
		"timeout":  45,
		"enabled":  "true",
		"factor":   2,
	}

	issues := CollectParamIssues(func() {
		if v, ok := IntParam(config, "priority"); !ok || v != 6 {
			t.Errorf("priority: %v %v", v, ok)
		}
		if v, ok := DurationParam(config, "timeout"); !ok || v != 45*time.Second {
			t.Errorf("timeout: %v %v", v, ok)
		}
		if v, ok := BoolParam(config, "enabled"); !ok || !v {
			t.Errorf("enabled: %v %v", v, ok)
		}
		if v, ok := FloatParam(config, "factor"); !ok || v != 2 {
			t.Errorf("factor: %v %v", v, ok)
		}
	})

	if len(issues) != 0 {
		t.Errorf("readable values must not be reported, got %+v", issues)
	}
}

// TestCollectParamIssues_NoCollectorIsHarmless: the helpers are called
// from everywhere, most of the time with nobody collecting.
func TestCollectParamIssues_NoCollectorIsHarmless(t *testing.T) {
	config := map[string]interface{}{"priority": "nope"}
	if _, ok := IntParam(config, "priority"); ok {
		t.Error("an unreadable value must still read as not-ok")
	}
}

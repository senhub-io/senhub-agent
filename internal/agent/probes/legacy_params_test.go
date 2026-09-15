package probes

import (
	"reflect"
	"testing"
)

func TestLegacyParamsUsedReportsOnlyWhatIsThere(t *testing.T) {
	RegisterLegacyParams("legacy-test", map[string]LegacyParam{
		"old_name":  {Replacement: "new_name", Accepted: true},
		"gone":      {Note: "the probe cannot do that"},
		"unrelated": {Replacement: "something"},
	})

	got := LegacyParamsUsed("legacy-test", map[string]interface{}{
		"gone":     10,
		"old_name": true,
		"current":  "fine",
	})
	// Sorted, so a report reads the same way twice.
	if want := []string{"gone", "old_name"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LegacyParamsUsed = %v, want %v", got, want)
	}

	if got := LegacyParamsUsed("legacy-test", nil); got != nil {
		t.Errorf("an empty params block reported %v", got)
	}
	if got := LegacyParamsUsed("no-such-probe", map[string]interface{}{"old_name": 1}); got != nil {
		t.Errorf("an undeclared probe type reported %v", got)
	}
}

// TestLegacyParamsForIsACopy: the registry describes what a probe once
// answered to. A caller reporting on it must not be able to edit it.
func TestLegacyParamsForIsACopy(t *testing.T) {
	RegisterLegacyParams("copy-test", map[string]LegacyParam{
		"old": {Replacement: "new", Accepted: true},
	})

	got := LegacyParamsFor("copy-test")
	delete(got, "old")
	got["injected"] = LegacyParam{}

	again := LegacyParamsFor("copy-test")
	if _, ok := again["old"]; !ok {
		t.Error("a caller deleted an entry from the registry")
	}
	if _, ok := again["injected"]; ok {
		t.Error("a caller added an entry to the registry")
	}
}

// TestTheShippedProbesDeclareTheirRenames is the guard against this
// mechanism existing and being empty. It names the two probes the
// mechanism was built for; a probe that changes a parameter name later
// adds itself here.
func TestTheShippedProbesDeclareTheirRenames(t *testing.T) {
	// The declarations live in the probe packages, which this package
	// cannot import (they import this one). The registry is populated by
	// their init(), so the test asserts on what a build with those probes
	// linked in reports — and skips honestly when they are not.
	for probeType, expected := range map[string][]string{
		"mysql":      {"expose_per_database", "expose_top_tables"},
		"postgresql": {"bloat_top_n", "expose_per_database", "expose_top_tables"},
	} {
		t.Run(probeType, func(t *testing.T) {
			if _, registered := LookupProbeConstructor(probeType); !registered {
				t.Skipf("probe %q is not in this build", probeType)
			}
			declared := LegacyParamsFor(probeType)
			if len(declared) == 0 {
				t.Fatalf("probe %q declares no legacy parameter names", probeType)
			}
			for _, name := range expected {
				if _, ok := declared[name]; !ok {
					t.Errorf("probe %q does not declare %q", probeType, name)
				}
			}
		})
	}
}

// TestEveryDeclarationSaysSomething: an entry with neither a
// replacement nor a note tells the operator a name is wrong and nothing
// about what to do, which is worse than silence.
func TestEveryDeclarationSaysSomething(t *testing.T) {
	for probeType, declared := range legacyParams {
		for name, p := range declared {
			if p.Replacement == "" && p.Note == "" {
				t.Errorf("%s.%s declares neither a replacement nor a note", probeType, name)
			}
		}
	}
}

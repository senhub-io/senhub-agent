package transformers

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// TestYAMLDefinitions_EveryMetricIsNameable pins what a sink needs before it
// can declare a metric at all.
//
// A definition is not documentation: Zabbix refuses a value for an item it
// was never told about, so the display name and the unit are what let the
// agent generate that item. PRTG reads the same two for its channel, and the
// docs page's parameter table is generated from them. A metric missing one
// arrives nowhere and reads, downstream, as a probe that collects nothing.
//
// Measured when this was written: 1313 metrics across 84 files, none missing
// any of the three. The guard exists so that stays true — a new probe is
// exactly when one gets forgotten, and the cost surfaces at a customer's
// console rather than here.
func TestYAMLDefinitions_EveryMetricIsNameable(t *testing.T) {
	entries, err := definitionFiles.ReadDir("definitions")
	if err != nil {
		t.Fatalf("reading the embedded definitions: %v", err)
	}

	type gap struct{ file, metric, missing string }
	var gaps []gap
	total := 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, readErr := definitionFiles.ReadFile("definitions/" + e.Name())
		if readErr != nil {
			t.Fatalf("reading %s: %v", e.Name(), readErr)
		}
		var def struct {
			Metrics []struct {
				Name        string `yaml:"name"`
				DisplayName string `yaml:"display_name"`
				Unit        string `yaml:"unit"`
			} `yaml:"metrics"`
		}
		if err := yaml.Unmarshal(raw, &def); err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		for _, m := range def.Metrics {
			total++
			var missing []string
			if strings.TrimSpace(m.DisplayName) == "" {
				missing = append(missing, "display_name")
			}
			if strings.TrimSpace(m.Unit) == "" {
				missing = append(missing, "unit")
			}
			if len(missing) > 0 {
				name := m.Name
				if name == "" {
					name = "(unnamed)"
				}
				gaps = append(gaps, gap{e.Name(), name, strings.Join(missing, " and ")})
			}
		}
	}

	if total == 0 {
		t.Fatal("no metric was read; the guard would pass on an empty set")
	}
	if len(gaps) == 0 {
		return
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].file < gaps[j].file })
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d metrics cannot be declared to a sink:\n", len(gaps), total)
	for _, g := range gaps {
		fmt.Fprintf(&b, "  %s: %s has no %s\n", g.file, g.metric, g.missing)
	}
	t.Error(b.String())
}

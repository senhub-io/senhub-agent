package transformers

import (
	"regexp"
	"testing"
)

var placeholderRe = regexp.MustCompile(`\{([^}]+)\}`)

// A display_name placeholder names a tag the series carries. One that no
// label of the metric declares is printed verbatim ("{node}") in every
// PRTG channel and Zabbix item built from it.
func TestDisplayNamePlaceholdersAreDeclaredLabels(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for probe, def := range defs {
		for _, m := range def.Metrics {
			declared := map[string]bool{}
			for _, l := range m.MultiInstanceLabels {
				declared[l] = true
			}
			for _, l := range def.MultiInstanceLabels {
				declared[l] = true
			}
			for _, ph := range placeholderRe.FindAllStringSubmatch(m.DisplayName, -1) {
				if !declared[ph[1]] {
					t.Errorf("%s/%s: display_name %q uses {%s}, not a declared label", probe, m.Name, m.DisplayName, ph[1])
				}
			}
		}
	}
}

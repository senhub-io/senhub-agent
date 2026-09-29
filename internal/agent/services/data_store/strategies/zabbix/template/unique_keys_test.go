package template

import (
	"sort"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Zabbix refuses to link a set of templates to a host when two of them
// declare the same item, prototype or discovery key, and it refuses the
// whole set, not the duplicate: a host given PostgreSQL and MySQL came up
// with no template at all, processor and memory included. Every template
// the generator can write must therefore be linkable beside every other.
func TestNoKeyIsDeclaredByTwoTemplates(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, platform := range []string{"linux", "windows"} {
		owner := map[string]string{}
		claim := func(key, probe string) {
			if prev, ok := owner[key]; ok && prev != probe {
				t.Errorf("%s: key %s is declared by both %s and %s; Zabbix would refuse to link them together", platform, key, prev, probe)
				return
			}
			owner[key] = probe
		}
		for _, n := range names {
			exp, err := Generate(defs[n], Options{Prefix: "senhub", Platform: platform})
			if err != nil {
				t.Fatalf("%s/%s: %v", platform, n, err)
			}
			for _, tpl := range exp.ZabbixExport.Templates {
				for _, it := range tpl.Items {
					claim(it.Key, n)
				}
				for _, r := range tpl.DiscoveryRules {
					claim(r.Key, n)
					for _, p := range r.ItemPrototypes {
						claim(p.Key, n)
					}
				}
			}
		}
		for _, tpl := range Base(Options{Prefix: "senhub", Platform: platform}).ZabbixExport.Templates {
			for _, it := range tpl.Items {
				claim(it.Key, "agent")
			}
		}
	}
}

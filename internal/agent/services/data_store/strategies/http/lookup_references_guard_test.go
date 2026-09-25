package http

import (
	"sort"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Every `lookup:` a definition writes must name a table the registry
// holds. A dangling id failed nowhere: the Zabbix template shipped the
// raw number with no value map and no trigger, PRTG had no lookup file to
// download, and Nagios read the metric as a plain number. The redfish
// health lookups lived in files nothing read, so a server's hardware
// health reached every output as 0 to 3 with nothing saying 2 is Critical.
func TestEveryDefinitionLookupResolves(t *testing.T) {
	logger := zerolog.Nop()
	reg, err := NewLookupRegistry(&logger)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	probes := make([]string, 0, len(defs))
	for p := range defs {
		probes = append(probes, p)
	}
	sort.Strings(probes)
	generator := NewPRTGLookupGenerator(reg)
	for _, p := range probes {
		for _, m := range defs[p].Metrics {
			if m.Lookup == "" {
				continue
			}
			if !reg.HasLookup(m.Lookup) {
				t.Errorf("%s.%s: lookup %q is not in lookups.yaml", p, m.Name, m.Lookup)
				continue
			}
			if err := generator.ValidateLookupForPRTG(m.Lookup); err != nil {
				t.Errorf("%s.%s: lookup %q: %v", p, m.Name, m.Lookup, err)
			}
		}
	}
}

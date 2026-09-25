package template

import (
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestEveryEmbeddedDefinitionGeneratesAnImportableTemplate holds the
// generator to what a live server enforces, over every definition, both
// platforms and both export formats.
//
// Importing all 82 templates into Zabbix 7.0 for the first time refused 11
// of them, for causes that had been in the tree for weeks: a friendly name
// with a slash, a discovery rule named after fifteen dimensions, the 6.0
// spelling of the group tag. Generation succeeded every time; only the
// server said no. This test is the server's part, offline.
func TestEveryEmbeddedDefinitionGeneratesAnImportableTemplate(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	var refused []string
	generated := 0
	for _, probe := range names {
		for _, platform := range []string{"", "linux", "windows"} {
			for _, version := range []string{"6.0", "7.0"} {
				exp, err := Generate(defs[probe], Options{Platform: platform, Version: version})
				if err != nil {
					t.Fatalf("%s (%s, %s): %v", probe, platform, version, err)
				}
				if exp.DeclaresNothing() {
					continue
				}
				generated++
				for _, why := range Validate(exp) {
					refused = append(refused, probe+" ("+platform+", "+version+"): "+why)
				}
			}
		}
	}
	if generated == 0 {
		t.Fatal("no template generated at all")
	}
	if len(refused) > 0 {
		t.Errorf("%d templates would be refused at import:\n  %s", len(refused), strings.Join(refused, "\n  "))
	}
}

// The validator must catch what the server catches; a validator that
// passes everything is worse than none, because it reads as a proof.
func TestValidateCatchesWhatAServerRefuses(t *testing.T) {
	long := strings.Repeat("x", 300)
	exp := Export{ZabbixExport: ExportBody{
		Version:        "7.0",
		TemplateGroups: []TemplateGroup{{Name: "Templates"}},
		Templates: []Template{{
			Template: "SenHub Bad / Name",
			Name:     "SenHub Bad / Name",
			DiscoveryRules: []DiscoveryRule{{
				Key: "senhub.discovery[x]", Name: long,
				ItemPrototypes: []ItemPrototype{{Key: "k", Name: long}},
			}},
			ValueMaps: []ValueMap{{Name: strings.Repeat("v", 65)}},
		}},
	}}
	got := Validate(exp)
	for _, want := range []string{"host name", "discovery rule", "prototype", "value map"} {
		found := false
		for _, g := range got {
			if strings.Contains(g, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("validator did not report the %s defect; got %q", want, got)
		}
	}

	wrongTag := Export{ZabbixExport: ExportBody{Version: "6.0", TemplateGroups: []TemplateGroup{{Name: "T"}}}}
	if got := Validate(wrongTag); len(got) != 1 || !strings.Contains(got[0], "`groups`") {
		t.Errorf("a 6.0 export with the 7.0 group tag must be refused; got %q", got)
	}
}

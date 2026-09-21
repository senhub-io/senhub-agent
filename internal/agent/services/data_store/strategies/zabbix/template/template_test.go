package template

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

type fakeLookups map[string]map[int]string

func (f fakeLookups) Lookup(id string) (map[int]string, bool) {
	m, ok := f[id]
	return m, ok
}

func diskDefinition() transformers.ProbeDefinition {
	return transformers.ProbeDefinition{
		ProbeName:    "logicaldisk",
		FriendlyName: "Logical disks",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "disk_free_mb", DisplayName: "Disk Free ({drive})", Unit: "MB", MultiInstanceLabels: []string{"drive"},
				Description: "Free space",
				Otel:        &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter", Attributes: map[string]string{"system.filesystem.state": "free"}},
			},
			{
				Name: "disk_used_mb", DisplayName: "Disk Used ({drive})", Unit: "MB", MultiInstanceLabels: []string{"drive"},
				Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter", Attributes: map[string]string{"system.filesystem.state": "used"}},
			},
			{
				Name: "disk_health", DisplayName: "Disk health", Unit: "#", Lookup: "sfs.generic.boolean",
				Otel: &transformers.OtelMapping{Name: "senhub.disk.health", Unit: "1", Type: "gauge", Expand: &transformers.ExpandDirective{Attribute: "hw.state", Mapping: map[string]int{"ok": 1, "failed": 0}}},
			},
			{
				Name: "disk_legacy", DisplayName: "Legacy", Unit: "#",
				Otel: &transformers.OtelMapping{Skip: true, Reason: "not mapped"},
			},
		},
	}
}

func TestGenerateWritesOneRulePerDimensionSetWithPrototypesUnderIt(t *testing.T) {
	exp, err := Generate(diskDefinition(), Options{Lookups: fakeLookups{"sfs.generic.boolean": {0: "No", 1: "Yes"}}})
	if err != nil {
		t.Fatal(err)
	}
	if exp.ZabbixExport.Version != "7.0" {
		t.Errorf("version = %s", exp.ZabbixExport.Version)
	}
	tpl := exp.ZabbixExport.Templates[0]
	if tpl.Template != "SenHub Logical disks" {
		t.Errorf("template = %s", tpl.Template)
	}
	if len(tpl.DiscoveryRules) != 2 {
		t.Fatalf("rules = %d, want the plain rule and the drive rule", len(tpl.DiscoveryRules))
	}
	byKey := map[string]DiscoveryRule{}
	for _, r := range tpl.DiscoveryRules {
		byKey[r.Key] = r
	}
	drive := byKey["senhub.discovery[logicaldisk,drive]"]
	if len(drive.ItemPrototypes) != 2 {
		t.Fatalf("drive prototypes = %+v", drive.ItemPrototypes)
	}
	free := drive.ItemPrototypes[0]
	if free.Key != "senhub.system.filesystem.usage[{#PROBE},{#DRIVE},free]" {
		t.Errorf("key = %s", free.Key)
	}
	if free.Name != "{#PROBE}: Disk Free ({#DRIVE})" || free.Units != "B" || free.Type != "ZABBIX_ACTIVE" || free.ValueType != "FLOAT" || free.Description != "Free space" {
		t.Errorf("prototype = %+v", free)
	}

	plain := byKey["senhub.discovery[logicaldisk]"]
	if len(plain.ItemPrototypes) != 1 {
		t.Fatalf("the skipped metric must not appear; plain prototypes = %+v", plain.ItemPrototypes)
	}
	health := plain.ItemPrototypes[0]
	if health.Key != "senhub.disk.health[{#PROBE}]" || health.ValueMap == nil || health.ValueMap.Name != "sfs.generic.boolean" || health.Units != "" {
		t.Errorf("enum prototype = %+v", health)
	}
	if len(tpl.ValueMaps) != 1 || tpl.ValueMaps[0].Mappings[0].Value != "0" || tpl.ValueMaps[0].Mappings[0].NewValue != "No" {
		t.Errorf("value maps = %+v", tpl.ValueMaps)
	}
}

func TestGenerateIsStableAcrossRuns(t *testing.T) {
	a, _ := Generate(diskDefinition(), Options{})
	b, _ := Generate(diskDefinition(), Options{})
	ya, _ := Encode(a)
	yb, _ := Encode(b)
	if string(ya) != string(yb) {
		t.Fatal("two generations of the same definition must be byte-identical (stable uuids)")
	}
	u := a.ZabbixExport.Templates[0].UUID
	if len(u) != 32 || u[12] != '4' || !strings.ContainsRune("89ab", rune(u[16])) {
		t.Errorf("uuid = %q, want the shape of a version 4 UUID, which Zabbix checks on import", u)
	}
}

func TestEncodeProducesTheImportLayout(t *testing.T) {
	exp, _ := Generate(diskDefinition(), Options{Version: "6.0", Prefix: "acme", ItemDelay: "30s", DiscoveryDelay: "10m", Group: "Templates/SenHub"})
	out, err := Encode(exp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{
		"zabbix_export:", "version: \"6.0\"", "template_groups:", "name: Templates/SenHub",
		"discovery_rules:", "key: acme.discovery[logicaldisk,drive]", "delay: 10m", "item_prototypes:",
		"key: acme.system.filesystem.usage[{#PROBE},{#DRIVE},free]", "delay: 30s", "value_type: FLOAT",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("export lacks %q\n%s", want, text)
		}
	}
}

func TestPrototypeNameDistinguishesCollapsedMetrics(t *testing.T) {
	m := transformers.MetricDefinition{Name: "x", DisplayName: "Usage", Otel: &transformers.OtelMapping{Name: "n", Attributes: map[string]string{"state": "free"}}}
	if got := prototypeName(m, nil); got != "{#PROBE}: Usage (free)" {
		t.Errorf("got %q", got)
	}
	m.DisplayName = "Free usage"
	if got := prototypeName(m, nil); got != "{#PROBE}: Free usage" {
		t.Errorf("a name that already says it is left alone: %q", got)
	}
}

func TestGenerateDeclaresAKeyOnceWhenTwoMetricsResolveToIt(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "disk", Metrics: []transformers.MetricDefinition{
		{Name: "free_mb", Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Attributes: map[string]string{"state": "free"}}},
		{Name: "free_bytes", Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Attributes: map[string]string{"state": "free"}}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(exp.ZabbixExport.Templates[0].DiscoveryRules[0].ItemPrototypes); n != 1 {
		t.Fatalf("%d prototypes for one key; Zabbix refuses a duplicate on import", n)
	}
}

func TestGenerateRefusesANamelessDefinition(t *testing.T) {
	if _, err := Generate(transformers.ProbeDefinition{}, Options{}); err == nil {
		t.Fatal("want an error")
	}
}

// A metric discovered per instance must name its instance. Without it,
// every prototype under one rule yields identically named items and an
// operator opening the host sees a column of the same line repeated.
// Seen on a bench: six filesystems, six items called the same thing.
func TestPrototypeNameCarriesTheDiscoveredInstance(t *testing.T) {
	m := transformers.MetricDefinition{Name: "x", DisplayName: "Control Plane Reachable"}

	if got := prototypeName(m, nil); got != "{#PROBE}: Control Plane Reachable" {
		t.Errorf("without a dimension the name is unchanged, got %q", got)
	}
	got := prototypeName(m, []string{"azure_app"})
	if got != "{#PROBE}: Control Plane Reachable ({#AZURE_APP})" {
		t.Errorf("name = %q, want the discovered instance appended", got)
	}
	// A display name that already carries a placeholder keeps its own
	// wording rather than being appended to twice.
	withPlaceholder := transformers.MetricDefinition{Name: "y", DisplayName: "Interface {interface} traffic"}
	got = prototypeName(withPlaceholder, []string{"interface"})
	if strings.Count(got, "{#INTERFACE}") != 1 {
		t.Errorf("name = %q, want the macro once", got)
	}
}

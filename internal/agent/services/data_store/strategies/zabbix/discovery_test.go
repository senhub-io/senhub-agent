package zabbix

import (
	"encoding/json"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func diskDefinition() *transformers.ProbeDefinition {
	return &transformers.ProbeDefinition{
		ProbeName: "logicaldisk",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "disk_free_mb", Unit: "MB", MultiInstanceLabels: []string{"device", "mount_point"},
				Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter", Attributes: map[string]string{"system.filesystem.state": "free"}},
			},
			{
				Name: "disk_count", Unit: "#",
				Otel: &transformers.OtelMapping{Name: "system.filesystem.count", Unit: "1", Type: "gauge"},
			},
		},
	}
}

type diskDefs struct{}

func (diskDefs) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	if probeType == "logicaldisk" {
		return diskDefinition()
	}
	return nil
}

func TestMacroForUsesTheZabbixMacroAlphabet(t *testing.T) {
	if got := macroFor("mount_point"); got != "{#MOUNT_POINT}" {
		t.Errorf("got %s", got)
	}
	if got := macroFor("db.name"); got != "{#DB_NAME}" {
		t.Errorf("got %s", got)
	}
}

func TestDiscoveryItemsListOneRulePerDimensionSetWithTheProbeAsAMacro(t *testing.T) {
	series := []otelmapper.CacheMetric{
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "disk_free_mb", Value: 1, Tags: map[string]string{"device": "/dev/sda1", "mount_point": "/"}},
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "disk_free_mb", Value: 2, Tags: map[string]string{"device": "/dev/sdb1", "mount_point": "/data"}},
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "disk_count", Value: 2, Tags: map[string]string{}},
		{ProbeName: "disks-2", ProbeType: "logicaldisk", MetricName: "disk_count", Value: 1, Tags: map[string]string{}},
	}
	items := discoveryItems("senhub", diskDefs{}, series)
	if len(items) != 2 {
		t.Fatalf("got %d rules, want one per dimension set: %+v", len(items), items)
	}

	byKey := map[string]string{}
	for _, it := range items {
		byKey[it.Key] = it.Value
	}
	var plain []map[string]string
	if err := json.Unmarshal([]byte(byKey["senhub.discovery[logicaldisk]"]), &plain); err != nil {
		t.Fatalf("plain rule: %v (%q)", err, byKey["senhub.discovery[logicaldisk]"])
	}
	if len(plain) != 2 || plain[0]["{#PROBE}"] != "disks" || plain[1]["{#PROBE}"] != "disks-2" {
		t.Errorf("the plain rule discovers the probe instances: %v", plain)
	}

	var dims []map[string]string
	if err := json.Unmarshal([]byte(byKey["senhub.discovery[logicaldisk,device,mount_point]"]), &dims); err != nil {
		t.Fatalf("dimension rule: %v", err)
	}
	if len(dims) != 2 || dims[0]["{#DEVICE}"] != "/dev/sda1" || dims[0]["{#MOUNT_POINT}"] != "/" || dims[0]["{#PROBE}"] != "disks" {
		t.Errorf("dimension rule rows = %v", dims)
	}
}

func TestDiscoveryItemsWithoutADefinitionDiscoverTheProbeOnly(t *testing.T) {
	items := discoveryItems("senhub", nil, []otelmapper.CacheMetric{
		{ProbeName: "p", ProbeType: "unknown", MetricName: "x", Tags: map[string]string{"a": "b"}},
	})
	if len(items) != 1 || items[0].Key != "senhub.discovery[unknown]" || items[0].Value != `[{"{#PROBE}":"p"}]` {
		t.Errorf("items = %+v", items)
	}
}

// A probe that publishes both an aggregate and one series per instance
// must not have its aggregate discovered as an instance: the item it
// creates is named with empty parentheses, which reads as a defect.
// Seen on the bench, where the Azure probe publishes its own totals
// beside one set per application.
func TestDiscoverySkipsTheAggregateSeries(t *testing.T) {
	defs := acaDefs{}
	metrics := []otelmapper.CacheMetric{
		{ProbeName: "aca", ProbeType: "azure_container_apps", MetricName: "azure_container_apps_up",
			Tags: map[string]string{"azure_app": "oltp"}},
		{ProbeName: "aca", ProbeType: "azure_container_apps", MetricName: "azure_container_apps_up",
			Tags: map[string]string{"azure_app": "billing"}},
		// L'agregat du collecteur : aucune application.
		{ProbeName: "aca", ProbeType: "azure_container_apps", MetricName: "azure_container_apps_up",
			Tags: map[string]string{}},
	}
	items := discoveryItems("senhub", defs, metrics)
	if len(items) != 1 {
		t.Fatalf("items = %d, want one discovery rule", len(items))
	}
	var rows []map[string]string
	if err := json.Unmarshal([]byte(items[0].Value), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("instances = %v, want only the two applications", rows)
	}
	for _, r := range rows {
		if r["{#AZURE_APP}"] == "" {
			t.Errorf("an instance with no application was discovered: %v", r)
		}
	}
}

// acaDefs is a probe that publishes one series per application beside
// its own aggregate.
type acaDefs struct{}

func (acaDefs) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	if probeType != "azure_container_apps" {
		return nil
	}
	return &transformers.ProbeDefinition{
		ProbeName: "azure_container_apps",
		Metrics: []transformers.MetricDefinition{{
			Name: "azure_container_apps_up", MultiInstanceLabels: []string{"azure_app"},
			Otel: &transformers.OtelMapping{Name: "senhub.azure_container_apps.up", Type: "gauge"},
		}},
	}
}

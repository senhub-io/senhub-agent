package zabbix

import (
	"encoding/json"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// filesystemDefinition declares a family of two: the used and the free
// bytes of a filesystem, one OTel name apart by one attribute. A Linux
// host feeds one of them and a Windows host the other, which is why the
// values are discovered rather than declared.
func filesystemDefinition() *transformers.ProbeDefinition {
	return &transformers.ProbeDefinition{
		ProbeName:           "logicaldisk",
		MultiInstanceLabels: []string{"mount_point"},
		Metrics: []transformers.MetricDefinition{
			{
				Name: "fs_used_bytes", DisplayName: "Used Bytes ({mount_point})",
				Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter", Attributes: map[string]string{"system.filesystem.state": "used"}},
			},
			{
				Name: "fs_free_bytes", DisplayName: "Free Bytes ({mount_point})",
				Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter", Attributes: map[string]string{"system.filesystem.state": "free"}},
			},
		},
	}
}

type fsDefs struct{}

func (fsDefs) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	if probeType == "logicaldisk" {
		return filesystemDefinition()
	}
	return nil
}

func rowsOf(t *testing.T, it item) []map[string]string {
	t.Helper()
	var rows []map[string]string
	if err := json.Unmarshal([]byte(it.Value), &rows); err != nil {
		t.Fatalf("%s: %v", it.Key, err)
	}
	return rows
}

func TestOnlyTheAttributeValuesTheHostFeedsAreDiscovered(t *testing.T) {
	// This host reports what its filesystems use and never what they
	// have free, which is the shape that used to leave half of the
	// generated prototypes empty forever.
	series := []otelmapper.CacheMetric{
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "fs_used_bytes", Value: 1, Tags: map[string]string{"mount_point": "/"}},
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "fs_used_bytes", Value: 2, Tags: map[string]string{"mount_point": "/data"}},
	}
	items := discoveryItems("senhub", fsDefs{}, series)
	if len(items) != 1 {
		t.Fatalf("rules = %+v, want the family's own rule alone", items)
	}
	if items[0].Key != "senhub.discovery.variants[logicaldisk,system.filesystem.usage,mount_point]" {
		t.Fatalf("rule key = %s", items[0].Key)
	}
	rows := rowsOf(t, items[0])
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want one per mount point", rows)
	}
	for _, r := range rows {
		if r["{#STATE}"] != "used" {
			t.Errorf("row %+v names a value this host never sends", r)
		}
		if r["{#PROBE}"] != "disks" {
			t.Errorf("row %+v lost the probe instance", r)
		}
	}
}

func TestBothValuesAreDiscoveredWhenBothAreFed(t *testing.T) {
	series := []otelmapper.CacheMetric{
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "fs_used_bytes", Value: 1, Tags: map[string]string{"mount_point": "/"}},
		{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "fs_free_bytes", Value: 9, Tags: map[string]string{"mount_point": "/"}},
	}
	items := discoveryItems("senhub", fsDefs{}, series)
	rows := rowsOf(t, items[0])
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want one per value fed", rows)
	}
	states := map[string]bool{}
	for _, r := range rows {
		states[r["{#STATE}"]] = true
	}
	if !states["used"] || !states["free"] {
		t.Fatalf("states = %+v, want both", states)
	}
}

func TestTheDiscoveredValueSubstitutesIntoTheKeyTheAgentSends(t *testing.T) {
	def := filesystemDefinition()
	cm := otelmapper.CacheMetric{ProbeName: "disks", ProbeType: "logicaldisk", MetricName: "fs_used_bytes", Value: 1, Tags: map[string]string{"mount_point": "/"}}
	sent := itemFor("senhub", def, cm).Key

	items := discoveryItems("senhub", fsDefs{}, []otelmapper.CacheMetric{cm})
	row := rowsOf(t, items[0])[0]

	// What the server builds from the prototype must be what arrives.
	built := "senhub.system.filesystem.usage[{#PROBE},{#MOUNT_POINT},{#STATE}]"
	for macro, value := range row {
		built = strings.ReplaceAll(built, macro, value)
	}
	if built != sent {
		t.Fatalf("the server would create %s but the agent sends %s", built, sent)
	}
}

func TestAnAggregateBesideItsPartsKeepsThemApart(t *testing.T) {
	// A metric with no attribute is not part of the family: it has its
	// own key and stays under the probe's own rule.
	def := filesystemDefinition()
	def.Metrics = append(def.Metrics, transformers.MetricDefinition{
		Name: "fs_total_bytes",
		Otel: &transformers.OtelMapping{Name: "system.filesystem.usage", Unit: "By", Type: "updowncounter"},
	})
	fams := familiesOf(def)
	if fams["fs_used_bytes"] == nil || fams["fs_free_bytes"] == nil {
		t.Fatal("the two members lost their family")
	}
	if fams["fs_total_bytes"] != nil {
		t.Fatal("the aggregate must not be discovered as a variant")
	}
}

func TestASoleMemberIsNotAFamily(t *testing.T) {
	def := filesystemDefinition()
	def.Metrics = def.Metrics[:1]
	if len(familiesOf(def)) != 0 {
		t.Fatal("one metric alone has nothing to discover")
	}
}

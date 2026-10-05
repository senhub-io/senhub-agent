package otelmapper

import (
	"math"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// The redfish probe emits sources in the BMC's units (MiB, MHz, percent,
// millivolts, Gbps). Pin that the definitions added for the formerly
// undefined names convert them to the OTel unit.
func TestRedfishDefinitionsConvertTheBMCUnits(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["redfish"]

	cases := []struct {
		metric string
		value  float64
		tags   map[string]string
		name   string
		want   float64
		attrs  map[string]string
	}{
		{"hardware.memory.capacity", 16384, map[string]string{"memory_id": "DIMM.A1"}, "hw.memory.size", 16384 * 1048576, map[string]string{"hw.id": "DIMM.A1"}},
		{"hardware.cpu.max_speed", 3500, map[string]string{"processor_id": "CPU1"}, "senhub.hardware.cpu.speed", 3.5e9, map[string]string{"senhub.hardware.cpu.speed.kind": "max"}},
		{"hardware.cpu.utilization", 42, map[string]string{"processor_id": "CPU1"}, "senhub.hardware.cpu.utilization", 0.42, map[string]string{"senhub.hardware.cpu.state": "total"}},
		{"hardware.memory.hpe.current_voltage", 1200, map[string]string{"memory_id": "DIMM.A1"}, "hw.voltage", 1.2, map[string]string{"hw.type": "memory"}},
		{"storage.drive.capacity_gb", 1, map[string]string{"drive_id": "d1"}, "hw.physical_disk.size", 1073741824, map[string]string{"senhub.hardware.source": "vendor"}},
		{"network.port.speed_gbps", 10, map[string]string{"port_name": "p1"}, "hw.network.bandwidth.limit", 1.25e9, nil},
		{"hardware.storage.pool.capacity.free", 100, map[string]string{"pool_name": "A"}, "senhub.hardware.storage.pool.usage", 100, map[string]string{"senhub.hardware.storage.pool.state": "free"}},
	}
	for _, c := range cases {
		recs, err := Resolve(&def, CacheMetric{
			ProbeName: "redfish", ProbeType: "redfish", MetricName: c.metric, Value: c.value, Tags: c.tags,
		}, DefaultResolveOptions())
		if err != nil || len(recs) != 1 {
			t.Errorf("%s: records = %+v, err %v", c.metric, recs, err)
			continue
		}
		r := recs[0]
		if r.Name != c.name || math.Abs(r.Value-c.want) > 1e-6*math.Max(1, math.Abs(c.want)) {
			t.Errorf("%s: got %s=%v, want %s=%v", c.metric, r.Name, r.Value, c.name, c.want)
		}
		for k, v := range c.attrs {
			if r.Attributes[k] != v {
				t.Errorf("%s: attribute %s = %q, want %q", c.metric, k, r.Attributes[k], v)
			}
		}
	}
}

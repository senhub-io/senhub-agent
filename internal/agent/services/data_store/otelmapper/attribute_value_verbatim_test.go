package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Only attribute and metric NAMES are sanitized, by each sink that needs
// it. A value is the device's own text: a drive a BMC calls
// "Lab drive 1 (failure predicted)" must reach OTLP under that name.
func TestAttributeValueKeepsItsCharacters(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["redfish"]
	const name = `Lab drive 1 (failure predicted), bay [3] #2 "a=b"`
	recs, err := Resolve(&def, CacheMetric{
		ProbeName: "redfish", ProbeType: "redfish", MetricName: "hardware.storage.drive.capacity.total",
		Value: 1, Tags: map[string]string{"drive_name": name, "drive_id": "Disk.Bay.1"},
	}, DefaultResolveOptions())
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %+v, err %v", recs, err)
	}
	if got := recs[0].Attributes["hw.name"]; got != name {
		t.Errorf("hw.name = %q, want %q", got, name)
	}
}

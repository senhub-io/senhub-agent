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

// A probe that sanitises a tag for the sinks that need it (PRTG channel
// names, URL filters, keys) ships the original in "<tag>_exact". The
// attribute carries the original, and the companion is not an attribute.
func TestAttributeUsesExactCompanionTag(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["redfish"]
	const exact = "Lab drive 1 (failure predicted)"
	m := CacheMetric{
		ProbeName: "redfish", ProbeType: "redfish", MetricName: "hardware.storage.drive.capacity.total",
		Value: 1, Tags: map[string]string{
			"drive_name": "Lab drive 1 failure predicted", "drive_name_exact": exact,
			"drive_id": "Disk.Bay.1", "vendor": "dell",
		},
	}
	for _, opts := range []ResolveOptions{DefaultResolveOptions(), {IncludeProbeTags: false}} {
		recs, err := Resolve(&def, m, opts)
		if err != nil || len(recs) != 1 {
			t.Fatalf("records = %+v, err %v", recs, err)
		}
		attrs := recs[0].Attributes
		if attrs["hw.name"] != exact {
			t.Errorf("hw.name = %q, want %q", attrs["hw.name"], exact)
		}
		if _, leaked := attrs["drive_name_exact"]; leaked {
			t.Errorf("companion tag leaked as an attribute: %v", attrs)
		}
	}

	// Without the companion the plain tag is used unchanged.
	m.Tags = map[string]string{"drive_name": "Bay 1", "drive_id": "d1"}
	recs, _ := Resolve(&def, m, DefaultResolveOptions())
	if len(recs) != 1 || recs[0].Attributes["hw.name"] != "Bay 1" {
		t.Errorf("plain tag not used: %+v", recs)
	}
}

package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// A drive whose health Redfish does not report emits its three convention
// states at 0: hw.state="unknown" is not a state the convention defines,
// so no backend reads it.
func TestAnUnreadableDriveHealthLeavesEveryStateAtZero(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["redfish"]
	recs, err := Resolve(&def, CacheMetric{ProbeName: "redfish", ProbeType: "redfish", MetricName: "hardware.storage.drive.health", Value: 3, Tags: map[string]string{"drive_id": "disk.bay.0"}}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records = %+v, want ok, degraded and failed", recs)
	}
	for _, r := range recs {
		if r.Attributes["hw.state"] == "unknown" || r.Value != 0 {
			t.Errorf("%s{hw.state=%q} = %v, want every convention state at 0", r.Name, r.Attributes["hw.state"], r.Value)
		}
	}
}

// A predicted drive failure is the predicted_failure series of the
// drive's hw.status, joined to its health series by hw.id.
func TestAPredictedDriveFailureIsAnHWState(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["redfish"]
	recs, err := Resolve(&def, CacheMetric{ProbeName: "redfish", ProbeType: "redfish", MetricName: "hardware.storage.drive.failure_predicted", Value: 1, Tags: map[string]string{"drive_id": "disk.bay.0"}}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %+v, want one", recs)
	}
	r := recs[0]
	if r.Name != "hw.status" || r.Attributes["hw.state"] != "predicted_failure" || r.Attributes["hw.type"] != "physical_disk" || r.Attributes["hw.id"] != "disk.bay.0" || r.Value != 1 {
		t.Fatalf("record = %+v, want hw.status{hw.type=physical_disk, hw.state=predicted_failure, hw.id=disk.bay.0} 1", r)
	}
}

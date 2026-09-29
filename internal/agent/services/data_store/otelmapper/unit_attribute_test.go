package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// system.cpu.utilization reached the OTLP backend as a ratio carrying the
// attribute unit="%", so a dashboard read 0.061 as 0.061 %.
func TestTheDisplayUnitIsNotAnAttribute(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["unifi"]
	recs, err := Resolve(&def, CacheMetric{
		ProbeName: "unifi", ProbeType: "unifi", MetricName: "unifi.device.cpu", Value: 6.1, Unit: "%",
		Tags: map[string]string{"unit": "%", "device_name": "switch", "device_type": "usw"},
	}, DefaultResolveOptions())
	if err != nil || len(recs) != 1 {
		t.Fatalf("records = %+v, err %v", recs, err)
	}
	if u, ok := recs[0].Attributes["unit"]; ok {
		t.Errorf("attribute unit=%q exported next to a value in %q", u, recs[0].Unit)
	}
	if recs[0].Unit != "1" || recs[0].Value < 0.0609 || recs[0].Value > 0.0611 {
		t.Errorf("record = %+v, want 0.061 in unit 1", recs[0])
	}
}

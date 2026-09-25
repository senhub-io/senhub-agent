package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// The Citrix probe reports the licence grace period left in hours, and the
// OTel metric is in seconds. The definition's unit said "custom", which
// the mapper cannot convert, so 48 hours left read as 48 seconds on every
// OTel-derived output.
func TestTheCitrixGracePeriodIsExportedInSeconds(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["citrix"]
	recs, err := Resolve(&def, CacheMetric{ProbeName: "citrix", ProbeType: "citrix", MetricName: "license_grace_hours_left", Value: 48.0}, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Value != 48*3600 {
		t.Fatalf("records = %+v, want one at 172800 s", recs)
	}
}

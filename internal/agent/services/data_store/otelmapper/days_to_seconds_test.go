package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Probes that count days until an expiry keep that display unit for PRTG
// and Nagios; the OTel record carries seconds, so an alert written as
// `< 30 * 86400` reads the same on every backend.
func TestExpiryInDaysIsExportedInSeconds(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		probe, metric, otelName string
		tags                    map[string]string
	}{
		{"netscaler", "netscaler.ssl.certificate.days_to_expiration", "senhub.netscaler.ssl.certificate.expiry", map[string]string{"certname": "www"}},
		{"veeam", "veeam_license_days_left", "senhub.veeam.license.expiry", map[string]string{}},
	}
	for _, c := range cases {
		def := defs[c.probe]
		recs, err := Resolve(&def, CacheMetric{
			ProbeName: c.probe, ProbeType: c.probe, MetricName: c.metric, Value: 30, Tags: c.tags,
		}, DefaultResolveOptions())
		if err != nil || len(recs) != 1 {
			t.Fatalf("%s: records = %+v, err %v", c.metric, recs, err)
		}
		r := recs[0]
		if r.Name != c.otelName || r.Unit != "s" || !floatApprox(r.Value, 30*86400) {
			t.Errorf("%s: got %s = %v %s, want %s = %v s", c.metric, r.Name, r.Value, r.Unit, c.otelName, 30*86400)
		}
	}
}

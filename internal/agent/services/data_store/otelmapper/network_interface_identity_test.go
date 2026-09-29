package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// On Windows the network probe's `interface` tag is the PDH instance (the
// adapter description) while network.interface.name is the connection
// name the network.interface entity is keyed on. The exported attribute
// must be the identity, or a series joins no entity.
func TestNetworkInterfaceNameIsTheIdentityNotThePDHInstance(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["network"]
	for _, metric := range []string{"bytes_sent", "bytes_received"} {
		recs, err := Resolve(&def, CacheMetric{
			ProbeName:  "network",
			ProbeType:  "network",
			MetricName: metric,
			Value:      1,
			Tags: map[string]string{
				"interface":              "Intel(R) Ethernet Connection _2",
				"network.interface.name": "Ethernet",
			},
		}, DefaultResolveOptions())
		if err != nil {
			t.Fatal(err)
		}
		if len(recs) != 1 {
			t.Fatalf("%s: records = %+v", metric, recs)
		}
		if got := recs[0].Attributes["network.interface.name"]; got != "Ethernet" {
			t.Errorf("%s: network.interface.name = %q, want the connection name", metric, got)
		}
	}
}

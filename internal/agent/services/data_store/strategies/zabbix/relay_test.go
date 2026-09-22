package zabbix

import (
	"encoding/json"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// relayed builds the shape the OTLP receiver hands over: a metric name
// the application chose, no definition anywhere in the agent, and the
// emitter's resource attributes folded onto the point as tags.
func relayed(service, metric string, value float64) otelmapper.CacheMetric {
	tags := map[string]string{"probe_name": "relay", "probe_type": "otlp_receiver", "otel_type": "gauge"}
	if service != "" {
		tags["service.name"] = service
	}
	return otelmapper.CacheMetric{
		ProbeName: "relay", ProbeType: "otlp_receiver", MetricName: metric,
		Value: value, Unit: "s", Tags: tags,
	}
}

// Two applications reporting the same metric name through one receiver
// must not share an item. They did: the key carried the receiving probe
// and nothing else, so the second value overwrote the first on an item
// that went on looking healthy.
func TestTwoEmittersRelayingOneMetricNameDoNotShareAnItem(t *testing.T) {
	checkout := itemFor("senhub", nil, relayed("checkout", "http.server.request.duration", 0.12))
	catalog := itemFor("senhub", nil, relayed("catalog", "http.server.request.duration", 0.34))

	if checkout.Key == catalog.Key {
		t.Fatalf("two services got the same key %s; one overwrites the other", checkout.Key)
	}
	if checkout.Key != "senhub.http.server.request.duration[relay,checkout]" {
		t.Errorf("key = %s", checkout.Key)
	}
}

// An emitter that named itself nothing is the shape the output already
// handled; it must keep its key rather than grow an empty parameter,
// which reads as a defect on the host page.
func TestAnUnnamedEmitterKeepsTheKeyItHad(t *testing.T) {
	it := itemFor("senhub", nil, relayed("", "http.server.request.duration", 0.12))
	if it.Key != "senhub.http.server.request.duration[relay]" {
		t.Errorf("key = %s", it.Key)
	}
}

// The discovery rule has to offer the services the receiver has seen,
// or the template generated for a relayed family has nothing to key its
// prototypes on.
func TestDiscoveryOffersOneInstancePerEmitter(t *testing.T) {
	items := discoveryItems("senhub", nil, []otelmapper.CacheMetric{
		relayed("checkout", "http.server.request.duration", 0.12),
		relayed("checkout", "jvm.memory.used", 1024),
		relayed("catalog", "http.server.request.duration", 0.34),
	})

	var value string
	for _, it := range items {
		if it.Key == "senhub.discovery[otlp_receiver,service.name]" {
			value = it.Value
		}
	}
	if value == "" {
		keys := make([]string, 0, len(items))
		for _, it := range items {
			keys = append(keys, it.Key)
		}
		t.Fatalf("no rule discovers the emitters; rules served: %s", strings.Join(keys, " "))
	}

	var instances []map[string]string
	if err := json.Unmarshal([]byte(value), &instances); err != nil {
		t.Fatalf("value %q: %v", value, err)
	}
	if len(instances) != 2 {
		t.Fatalf("discovered %d instances, want the two services: %v", len(instances), instances)
	}
	seen := map[string]bool{}
	for _, inst := range instances {
		if inst["{#PROBE}"] != "relay" {
			t.Errorf("instance %v does not name the probe", inst)
		}
		seen[inst["{#SERVICE_NAME}"]] = true
	}
	if !seen["checkout"] || !seen["catalog"] {
		t.Errorf("instances = %v, want one per service", instances)
	}
}

// A metric the agent does describe keeps the dimensions its definition
// gives it, whatever the emitter tags say.
func TestADefinedMetricIsNotReshapedByAnEmitterTag(t *testing.T) {
	cm := otelmapper.CacheMetric{
		ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.usage", Value: 42.5, Unit: "%",
		Tags: map[string]string{"cpu": "3", "service.name": "somebody", "probe_name": "host-cpu", "probe_type": "cpu"},
	}
	if it := itemFor("senhub", cpuDefinition(), cm); it.Key != "senhub.system.cpu.utilization[host-cpu,3,user]" {
		t.Errorf("key = %s", it.Key)
	}
}

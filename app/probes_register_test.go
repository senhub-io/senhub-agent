package app

import (
	"sort"
	"testing"

	"senhub-agent.go/internal/agent/probes"
)

// TestOSSBuildRegistersOnlyPublicProbes locks the open-source probe
// catalogue. The default agent binary built from this public repository
// must register exactly the host-local probes whose source lives here,
// and must NOT register any probe that belongs to the
// senhub-agent-enterprise module.
//
// A failure means app/probes_register.go drifted: either the OSS edition
// silently grew a paid probe (a paid import crept back in — which would
// also panic at runtime with a duplicate registration once the
// enterprise entrypoint adds it), or it lost a public one.
func TestOSSBuildRegistersOnlyPublicProbes(t *testing.T) {
	want := []string{
		"apache",
		"activemq",
		"consul",
		"ceph",
		"cassandra",
		"couchdb",
		"clickhouse",
		"chrony",
		"cpu",
		"dns_latency",
		"envoy",
		"elasticsearch",
		"event",
		"exec",
		"filetail",
		"haproxy",
		"http_check",
		"hyperv",
		"icmp_check",
		"kafka",
		"jenkins",
		"influxdb",
		"ipmi",
		"kubernetes",
		"linux_logs",
		"logicaldisk",
		"memcached",
		"memory",
		"nats",
		"mysql",
		"network",
		"nginx",
		"opensearch",
		"nvidia",
		"oracle",
		"otlp_receiver",
		"phpfpm",
		"postgresql",
		"process",
		"prometheus_scrape",
		"rabbitmq",
		"pulsar",
		"proxmox",
		"snmp_poll",
		"snmp_trap",
		"solr",
		"syslog",
		"systemd",
		"tcp_dial",
		"varnish",
		"unifi",
		"wifi_signal_strength",
		"wildfly",
		"windows_eventlog",
		"winservices",
	}
	sort.Strings(want)

	got := probes.RegisteredProbeNames() // already sorted by the registry
	if len(got) != len(want) {
		t.Fatalf("OSS build registers %d probes %v; want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OSS probe catalogue = %v; want %v", got, want)
		}
	}

	// Defence in depth: name the enterprise probes explicitly so the
	// guard reads as documentation of what must never ship in the OSS
	// binary. These live only in senhub-agent-enterprise.
	forbidden := []string{
		"citrix",
		"veeam",
		"netscaler",
		"redfish",
		"ibmi",
		"postgresql",
		"mysql",
		"ping_gateway",
		"ping_webapp",
		"load_webapp",
	}
	registered := probes.GetRegisteredProbeTypes()
	for _, name := range forbidden {
		if registered[name] {
			t.Errorf("enterprise probe %q is registered in the OSS build — it must live only in senhub-agent-enterprise", name)
		}
	}
}

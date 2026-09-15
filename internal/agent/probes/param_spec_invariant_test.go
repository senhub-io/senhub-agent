package probes_test

import (
	"testing"

	"senhub-agent.go/internal/agent/probes"
	_ "senhub-agent.go/internal/agent/probes/activemq"
	_ "senhub-agent.go/internal/agent/probes/apache"
	_ "senhub-agent.go/internal/agent/probes/cassandra"
	_ "senhub-agent.go/internal/agent/probes/ceph"
	_ "senhub-agent.go/internal/agent/probes/chrony"
	_ "senhub-agent.go/internal/agent/probes/clickhouse"
	_ "senhub-agent.go/internal/agent/probes/consul"
	_ "senhub-agent.go/internal/agent/probes/couchdb"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/dnslatency"
	_ "senhub-agent.go/internal/agent/probes/docker"
	_ "senhub-agent.go/internal/agent/probes/elasticsearch"
	_ "senhub-agent.go/internal/agent/probes/envoy"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/execprobe"
	_ "senhub-agent.go/internal/agent/probes/filetail"
	_ "senhub-agent.go/internal/agent/probes/haproxy"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/httpcheck"
	_ "senhub-agent.go/internal/agent/probes/hyperv"
	_ "senhub-agent.go/internal/agent/probes/icmpcheck"
	_ "senhub-agent.go/internal/agent/probes/influxdb"
	_ "senhub-agent.go/internal/agent/probes/ipmi"
	_ "senhub-agent.go/internal/agent/probes/jenkins"
	_ "senhub-agent.go/internal/agent/probes/kafka"
	_ "senhub-agent.go/internal/agent/probes/kubernetes"
	_ "senhub-agent.go/internal/agent/probes/linuxlogs"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memcached"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/modbus"
	_ "senhub-agent.go/internal/agent/probes/mongodb"
	_ "senhub-agent.go/internal/agent/probes/mssql"
	_ "senhub-agent.go/internal/agent/probes/mysql"
	_ "senhub-agent.go/internal/agent/probes/nats"
	_ "senhub-agent.go/internal/agent/probes/network"
	_ "senhub-agent.go/internal/agent/probes/nginx"
	_ "senhub-agent.go/internal/agent/probes/ntp"
	_ "senhub-agent.go/internal/agent/probes/nvidia"
	_ "senhub-agent.go/internal/agent/probes/opensearch"
	_ "senhub-agent.go/internal/agent/probes/oracle"
	_ "senhub-agent.go/internal/agent/probes/osupdates"
	_ "senhub-agent.go/internal/agent/probes/otlpreceiver"
	_ "senhub-agent.go/internal/agent/probes/phpfpm"
	_ "senhub-agent.go/internal/agent/probes/postgresql"
	_ "senhub-agent.go/internal/agent/probes/process"
	_ "senhub-agent.go/internal/agent/probes/promscrape"
	_ "senhub-agent.go/internal/agent/probes/proxmox"
	_ "senhub-agent.go/internal/agent/probes/pulsar"
	_ "senhub-agent.go/internal/agent/probes/rabbitmq"
	_ "senhub-agent.go/internal/agent/probes/redis"
	_ "senhub-agent.go/internal/agent/probes/smart"
	_ "senhub-agent.go/internal/agent/probes/snmppoll"
	_ "senhub-agent.go/internal/agent/probes/snmptrap"
	_ "senhub-agent.go/internal/agent/probes/solr"
	_ "senhub-agent.go/internal/agent/probes/swarm"
	_ "senhub-agent.go/internal/agent/probes/syslog"
	_ "senhub-agent.go/internal/agent/probes/systemd"
	_ "senhub-agent.go/internal/agent/probes/tcpdial"
	_ "senhub-agent.go/internal/agent/probes/tomcat"
	_ "senhub-agent.go/internal/agent/probes/unifi"
	_ "senhub-agent.go/internal/agent/probes/varnish"
	_ "senhub-agent.go/internal/agent/probes/wildfly"
	_ "senhub-agent.go/internal/agent/probes/windowseventlog"
	_ "senhub-agent.go/internal/agent/probes/winservices"
	_ "senhub-agent.go/internal/agent/probes/zookeeper"
)

// A declared schema must describe a probe that exists, carry a display
// name and a docs page, and only default to values its own kind accepts.
func TestProbeSpecs_DescribeRegisteredProbes(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()
	specs := probes.RegisteredProbeSpecs()
	if len(specs) == 0 {
		t.Fatal("no probe spec registered; the imports above should register at least the host probes")
	}
	for _, s := range specs {
		if !registered[s.Type] {
			t.Errorf("spec %q describes a probe type that is not registered", s.Type)
		}
		if s.DisplayName == "" || s.DocsPath == "" {
			t.Errorf("spec %q needs a DisplayName and a DocsPath", s.Type)
		}
		if problems := s.CheckParams(map[string]interface{}{}); len(problems) != 0 && !hasRequired(s) {
			t.Errorf("spec %q rejects an empty params map without declaring a required key: %v", s.Type, problems)
		}
		// A probe that observes one server per instance needs something
		// from the operator to start; without a declared start set the
		// console falls back to a guess and the form opens on optional
		// settings.
		if _, fine := startsWithDefaults[s.Type]; s.MultiInstance && !s.HasStartSet() && !fine {
			t.Errorf("spec %q is multi-instance but declares no Required or Essential parameter", s.Type)
		}
		checkConditions(t, s)
	}
}

// startsWithDefaults lists the multi-instance probes that do something
// useful with every parameter left at its default, with the reason.
var startsWithDefaults = map[string]string{
	"linux_logs": "follows every journal unit when none is listed",
	"kubernetes": "reads the cluster it runs in when no kubeconfig is given; a second instance names another cluster's kubeconfig",
}

// A condition must name a sibling parameter and, when that sibling has
// an enum, only values of it: a typo there would hide credentials the
// operator has to type.
func checkConditions(t *testing.T, s probes.ProbeSpec) {
	t.Helper()
	byKey := map[string]probes.ParamSpec{}
	for _, p := range s.Params {
		byKey[p.Key] = p
	}
	for _, p := range s.Params {
		for _, c := range p.EssentialWhen {
			ref, ok := byKey[c.Key]
			if !ok {
				t.Errorf("spec %q: %s is essential when %q, which is not a parameter", s.Type, p.Key, c.Key)
				continue
			}
			if len(c.Values) == 0 {
				t.Errorf("spec %q: %s has a condition on %s with no values", s.Type, p.Key, c.Key)
			}
			for _, v := range c.Values {
				if len(ref.Enum) > 0 && !containsString(ref.Enum, v) {
					t.Errorf("spec %q: %s is essential when %s is %q, which %s cannot be", s.Type, p.Key, c.Key, v, c.Key)
				}
			}
		}
	}
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func hasRequired(s probes.ProbeSpec) bool {
	for _, p := range s.Params {
		if p.Required {
			return true
		}
	}
	return false
}

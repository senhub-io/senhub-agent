package probes_test

// External-test-package suite (note `package probes_test`, not
// `package probes`). The blank imports below are how every probe
// package's init() — which calls `probes.RegisterProbe(...)` — fires
// when running `go test ./internal/agent/probes/`. The internal
// `package probes` cannot do this itself: a probe package imports
// the `probes` parent for RegisterProbe, so the parent importing the
// probe package back would be a cycle. The external test package is
// the standard Go pattern for breaking that dependency loop while
// keeping the tests close to the code they exercise.

import (
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"

	_ "senhub-agent.go/internal/agent/probes/activemq"
	_ "senhub-agent.go/internal/agent/probes/apache"
	_ "senhub-agent.go/internal/agent/probes/cassandra"
	_ "senhub-agent.go/internal/agent/probes/ceph"
	_ "senhub-agent.go/internal/agent/probes/chrony"
	_ "senhub-agent.go/internal/agent/probes/clickhouse"
	_ "senhub-agent.go/internal/agent/probes/consul"
	_ "senhub-agent.go/internal/agent/probes/couchdb"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/elasticsearch"
	_ "senhub-agent.go/internal/agent/probes/envoy"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/filetail"
	_ "senhub-agent.go/internal/agent/probes/haproxy"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/hyperv"
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
	_ "senhub-agent.go/internal/agent/probes/nvidia"
	_ "senhub-agent.go/internal/agent/probes/opensearch"
	_ "senhub-agent.go/internal/agent/probes/oracle"
	_ "senhub-agent.go/internal/agent/probes/otlpreceiver"
	_ "senhub-agent.go/internal/agent/probes/phpfpm"
	_ "senhub-agent.go/internal/agent/probes/postgresql"
	_ "senhub-agent.go/internal/agent/probes/process"
	_ "senhub-agent.go/internal/agent/probes/proxmox"
	_ "senhub-agent.go/internal/agent/probes/pulsar"
	_ "senhub-agent.go/internal/agent/probes/rabbitmq"
	_ "senhub-agent.go/internal/agent/probes/smart"
	_ "senhub-agent.go/internal/agent/probes/snmppoll"
	_ "senhub-agent.go/internal/agent/probes/snmptrap"
	_ "senhub-agent.go/internal/agent/probes/solr"
	_ "senhub-agent.go/internal/agent/probes/syslog"
	_ "senhub-agent.go/internal/agent/probes/systemd"
	_ "senhub-agent.go/internal/agent/probes/tomcat"
	_ "senhub-agent.go/internal/agent/probes/unifi"
	_ "senhub-agent.go/internal/agent/probes/varnish"
	_ "senhub-agent.go/internal/agent/probes/wildfly"
	_ "senhub-agent.go/internal/agent/probes/winservices"
	_ "senhub-agent.go/internal/agent/probes/zookeeper"
)

// TestEveryRegisteredProbeIsAuthorizable enforces a structural
// invariant across the runtime probe registry and the licence
// catalogues — every probe added to the registry MUST also be
// authorizable by at least one licence mechanism:
//
//   - probes package:                                    populated via probes.RegisterProbe
//   - internal/agent/services/license/license.go:        freeTierProbes
//   - internal/agent/services/license/probe_catalog.go:  paidProbes
//
// Without this guard, a probe wired into the registry without
// touching either licence file ships in the binary but no JWT
// licence can authorize it — the validator silently drops the probe
// at runtime even when the licence claims look correct.
//
// When this test fails: add the probe to one of:
//
//   - freeTierProbes (host-local observability, no remote system involved)
//   - paidProbes     (paid probe — claimable by a JWT licence)
func TestEveryRegisteredProbeIsAuthorizable(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()

	// Guard against the silent regression "no probe registered at all".
	// Pre-refactor, the registry was a hardcoded map and the test
	// trivially held when the map was emptied; the self-registration
	// pattern needs an explicit assertion that the catalogue is alive.
	if len(registered) == 0 {
		t.Fatalf("probes.GetRegisteredProbeTypes() returned an empty map — " +
			"no probe package's init() ran. " +
			"Check the blank imports in this test file and in app/probes_register.go")
	}

	var orphans []string
	for name := range registered {
		if !license.IsProbeAuthorizable(name) {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("probes registered in registry but neither in freeTierProbes nor paidProbes: %s\n\n"+
			"add each to one of:\n"+
			"  - internal/agent/services/license/license.go (freeTierProbes)        — host-local\n"+
			"  - internal/agent/services/license/probe_catalog.go (paidProbes)      — paid, claimable by JWT",
			strings.Join(orphans, ", "))
	}
}

// The reverse invariant — every paid-catalogue entry MUST be backed by
// a registered probe (formerly TestNoStalePaidProbe) — does not hold in
// the open-source build: the paid catalogue stays in core so the licence
// validator recognises the names a JWT may grant, but the paid probe
// packages live in the senhub-agent-enterprise module and are not
// registered here. That completeness check belongs to the enterprise
// repository's test suite, where all probes are registered (see #183).
// The OSS direction — no paid probe leaks into the public binary — is
// guarded by TestOSSBuildRegistersOnlyPublicProbes in the app package.

// TestEveryRegisteredProbeHasEntitySource enforces that every probe participates
// in Toise topology inventory. A nil EntitySource() is a contract violation —
// the probe won't appear in the entity graph and its metrics can't be attributed
// to an infrastructure node in Toise.
//
// Host-level probes and log conduits satisfy the invariant via the BaseProbe
// fallback (NoOpEntitySource). Remote-target probes MUST call SetEntitySource()
// in their constructor with a real Source.
//
// When this test fails:
//   - If the probe embeds *types.BaseProbe: verify SetEntitySource() is called
//     in the constructor, or that the probe is host-level (inherits NoOpEntitySource).
//   - If the probe does NOT embed *types.BaseProbe: add EntitySource() to the
//     concrete type. See .claude/rules/probes.md §Entity source (mandatory wiring step 5).
func TestEveryRegisteredProbeHasEntitySource(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()
	if len(registered) == 0 {
		t.Fatalf("probes.GetRegisteredProbeTypes() returned an empty map — " +
			"no probe package's init() ran. " +
			"Check the blank imports in this test file.")
	}

	log := logger.NewLogger(&cliArgs.ParsedArgs{})

	// Probes that legitimately return the NoOpEntitySource: they describe no
	// distinct REMOTE entity, so the host the agent runs on (emitted by the
	// entity foundation) already covers them. Anything NOT here is a remote-
	// target probe and, when it is constructable on a bare config, MUST expose
	// a real (non-NoOp) source — a NoOp there is the #471 "false confidence"
	// failure mode (a probe that monitors a Kafka/DB/device but registers no
	// node). Keep this list in sync with the host-local/conduit/hardware
	// classification in .claude/rules/probes.md.
	hostLocalOrConduit := map[string]bool{
		// host self-observability
		"cpu": true, "memory": true, "network": true, "logicaldisk": true,
		"wifi_signal_strength": true,
		// log conduits
		"linux_logs": true, "syslog": true, "filetail": true,
		"windows_eventlog": true, "event": true,
		// host hardware = host facet, not an entity (D2 / #456)
		"smart": true, "ipmi": true, "nvidia": true,
		// command/receiver/synthetic-check probes — no owned remote entity
		"exec": true, "otlp_receiver": true, "snmp_trap": true,
		"prometheus_scrape": true,
		"http_check":        true, "icmp_check": true, "tcp_dial": true, "dns_latency": true,
		// host-scoped inventory probes: they describe the local host's units/
		// services/processes/clock, not a distinct remote entity (process emits
		// process entities only in opt-in inventory mode, NoOp on a bare config).
		"chrony": true, "process": true, "systemd": true, "winservices": true,
		// kubernetes monitors a remote cluster but does not yet emit a
		// service.instance for it — allowlisted until it does (tracked in #482).
		"kubernetes": true,
		// hyperv is //go:build windows; on the non-windows test host its
		// constructor is the no-op stub (NoOpEntitySource). Its real
		// compute.vm source lives in probe_windows.go and is exercised on the
		// Windows CI runner, not here.
		"hyperv": true,
	}

	for name := range registered {
		name := name
		t.Run(name, func(t *testing.T) {
			ctor, ok := probes.LookupProbeConstructor(name)
			if !ok {
				t.Fatalf("probe %q: registered but LookupProbeConstructor returned false", name)
			}
			probe, err := ctor(map[string]interface{}{"interval": 30}, log)
			if err != nil {
				// LIMITATION: most remote-target probes require real config
				// (host/endpoint/credentials) to construct, so we cannot inspect
				// their EntitySource() here. Per-probe valid-config fixtures that
				// would lift this skip are tracked in #482.
				t.Skipf("probe %q requires real config to construct: %v", name, err)
				return
			}
			src := probe.EntitySource()
			if src == nil {
				t.Errorf("probe %q: EntitySource() returned nil — must embed "+
					"*types.BaseProbe or implement EntitySource().", name)
				return
			}
			_, isNoOp := src.(types.NoOpEntitySource)
			if isNoOp && !hostLocalOrConduit[name] {
				t.Errorf("probe %q is a remote-target probe but EntitySource() is the "+
					"NoOpEntitySource — it monitors a distinct entity yet registers no node "+
					"in Toise topology. Call SetEntitySource() in the constructor with the "+
					"probe's real Source, or add the probe to the host-local/conduit allowlist "+
					"if it genuinely describes no remote entity (#471).", name)
			}
		})
	}
}

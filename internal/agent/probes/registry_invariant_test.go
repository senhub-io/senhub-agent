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

	for name := range registered {
		name := name
		t.Run(name, func(t *testing.T) {
			ctor, ok := probes.LookupProbeConstructor(name)
			if !ok {
				t.Fatalf("probe %q: registered but LookupProbeConstructor returned false", name)
			}
			probe, err := ctor(map[string]interface{}{"interval": 30}, log)
			if err != nil {
				// Construction error is acceptable (missing required config like host/url).
				// The entity source check is best-effort for probes requiring real config.
				t.Skipf("probe %q requires real config to construct: %v", name, err)
				return
			}
			if probe.EntitySource() == nil {
				t.Errorf("probe %q: EntitySource() returned nil — "+
					"every probe MUST call SetEntitySource() in its constructor, or "+
					"embed *types.BaseProbe (which returns NoOpEntitySource by default). "+
					"See .claude/rules/probes.md §Entity source (mandatory wiring step 5).", name)
			}
		})
	}
}

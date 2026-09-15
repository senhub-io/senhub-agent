package probes_test

// External-test-package suite (note `package probes_test`, not
// `package probes`). The internal `package probes` cannot host these
// tests: a probe package imports the `probes` parent for RegisterProbe,
// so the parent importing the probe packages back would be a cycle. The
// external test package is the standard Go pattern for breaking that
// loop while keeping the tests close to the code they exercise.
//
// The registry is populated by blank-importing `app`, whose
// probes_register.go is the canonical registration list of the probes
// the OSS binary ships — each of those packages' init() calls
// probes.RegisterProbe. Deriving the set from that one list is the
// point: this file used to repeat the catalogue as its own blank-import
// list, the two drifted (nine probes shipped but were never imported
// here, escaping every invariant below), and the repair was a third
// test that parsed both lists and compared them. One list cannot drift
// from itself.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"

	_ "senhub-agent.go/app"
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
// repository's test suite, where all probes are registered (see #183,
// #489). The OSS direction — no paid probe leaks into the public binary
// — is guarded by TestOSSBuildRegistersOnlyPublicProbes in the app
// package.

// TestEveryFreeTierProbeHasRegisteredConstructor is the free-tier half of
// #489: every name declared in freeTierProbes (license.go) MUST resolve to
// a registered probe constructor. The #484 bug shipped green because no
// test cross-checked the licence catalogue against the registry — four
// free-tier probes were declared but their packages never blank-imported
// in app/probes_register.go, so the names existed for the licence
// validator while the binary could not construct them. The registry this
// test reads is populated from app/probes_register.go itself, so a pass
// here covers the shipped binary, not just the test binary. The
// paid-catalogue half runs in the enterprise suite (see the comment
// above).
func TestEveryFreeTierProbeHasRegisteredConstructor(t *testing.T) {
	free := license.GetFreeTierProbes()
	if len(free) == 0 {
		t.Fatal("license.GetFreeTierProbes() returned no probes — free-tier catalogue is empty?")
	}
	sort.Strings(free)

	var unregistered []string
	for _, name := range free {
		if _, ok := probes.LookupProbeConstructor(name); !ok {
			unregistered = append(unregistered, name)
		}
	}
	if len(unregistered) > 0 {
		t.Fatalf("freeTierProbes entries with no registered constructor: %s\n\n"+
			"each name in freeTierProbes (internal/agent/services/license/license.go) must match "+
			"a probe registered via probes.RegisterProbe — check the probe package's init() and "+
			"the blank imports in app/probes_register.go (#484/#489).",
			strings.Join(unregistered, ", "))
	}
}

// probeConfigFixtures supplies the minimal valid config each config-requiring
// probe needs to construct, so TestEveryRegisteredProbeHasEntitySource can
// build it and inspect EntitySource() instead of skipping it (#482). The
// values describe an unreachable target (RFC 5737 / RFC 6598 documentation
// addresses): the constructor only parses and validates the config, it does
// NOT dial the target — that happens in OnStart/Collect, which the invariant
// never calls. A probe absent from this table constructs on a bare
// {"interval": 30} config.
//
// When a new config-requiring probe is added and this test fails with
// "failed to construct with its fixture", add the minimal config here.
var probeConfigFixtures = map[string]map[string]interface{}{
	"ceph":     {"username": "admin", "password": "secret"},
	"filetail": {"paths": []interface{}{"/var/log/app.log"}},
	"jenkins":  {"endpoint": "https://jenkins.example.com"},
	"modbus": {"host": "192.0.2.10", "registers": []interface{}{
		map[string]interface{}{"name": "reg0", "address": 0, "type": "uint16"},
	}},
	"mssql":      {"host": "192.0.2.10"},
	"oracle":     {"host": "192.0.2.10", "service_name": "ORCL", "username": "system"},
	"postgresql": {"host": "192.0.2.10", "username": "postgres", "password": "secret"},
	"proxmox":    {"endpoint": "https://192.0.2.10:8006", "token_id": "user@pam!mon", "token_secret": "secret"},
	"snmp_poll": {"target": "192.0.2.10", "custom_mappings": []interface{}{
		map[string]interface{}{"oid": "1.3.6.1.2.1.1.3.0", "metric": "sysuptime"},
	}},
	"unifi": {"username": "admin", "password": "secret"},
	// Synthetic-check / collection probes that require a target list to construct.
	// Values are RFC 5737 documentation addresses / example names: the constructor
	// only parses them, it never dials.
	"dns_latency":       {"names": []interface{}{"example.com"}},
	"http_check":        {"targets": []interface{}{"http://192.0.2.1/"}},
	"icmp_check":        {"targets": []interface{}{"192.0.2.1"}},
	"ntp":               {"servers": []interface{}{"192.0.2.1"}},
	"prometheus_scrape": {"targets": []interface{}{"http://192.0.2.1:9100/metrics"}},
	"tcp_dial":          {"targets": []interface{}{"192.0.2.1:80"}},
	"windows_eventlog":  {"channels": []interface{}{"System"}},
}

// The exec probe validates at construction that its command is an existing,
// non-world-writable executable, so no hard-coded path is portable across the
// linux and windows CI runners. Point it at the test binary itself, which
// always exists, is executable, and is an absolute path on every platform.
func init() {
	if exe, err := os.Executable(); err == nil {
		probeConfigFixtures["exec"] = map[string]interface{}{"command": exe}
	}
}

// TestEveryRegisteredProbeHasEntitySource enforces that every probe participates
// in Toise topology inventory. A nil EntitySource() is a contract violation —
// the probe won't appear in the entity graph and its metrics can't be attributed
// to an infrastructure node in Toise.
//
// Host-level probes and log conduits satisfy the invariant via the BaseProbe
// fallback (NoOpEntitySource). Remote-target probes MUST call SetEntitySource()
// in their constructor with a real Source. Since #471 the ProbePoller itself
// registers EntitySource() with the detector (see
// TestProbePollerRegistersEntitySourceOnStart), so the source inspected here
// is by construction the one polled at runtime — probes have no other
// registration path (TestProbePackagesDoNotRegisterEntitySourcesDirectly).
//
// Probes that require config (host/endpoint/credentials) to construct are
// driven through probeConfigFixtures: a minimal valid config per probe so the
// invariant can build them and inspect EntitySource() rather than skipping. The
// constructor only parses config; it does not dial the target, so the fixtures
// point at unreachable documentation addresses.
//
// When this test fails:
//   - "failed to construct with its fixture": add a minimal valid-config entry
//     for the probe to probeConfigFixtures.
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
		// os_updates reads the local package backend (apt/dnf/WUA) —
		// host patch posture, a host facet like cpu/memory/logicaldisk.
		"os_updates": true,
		// command/receiver/synthetic-check probes — no owned remote entity
		"exec": true, "otlp_receiver": true, "snmp_trap": true,
		"prometheus_scrape": true,
		"http_check":        true, "icmp_check": true, "tcp_dial": true, "dns_latency": true,
		// ntp measures THIS host's clock error; the server it asks is a
		// reference, not a monitored system, and its only stable identity
		// would be an address — which the contract forbids as an identity.
		"ntp": true,
		// host-scoped inventory probes that are NoOp on a bare config: process
		// emits process entities only in opt-in inventory mode; systemd builds
		// its unit source in the linux constructor only (the non-linux stub is
		// NoOp, and this test runs on darwin/windows too). chrony and
		// winservices declare their source unconditionally, so they are NOT
		// allowlisted — a NoOp there is a regression.
		"process": true, "systemd": true,
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
			cfg := map[string]interface{}{"interval": 30}
			for k, v := range probeConfigFixtures[name] {
				cfg[k] = v
			}
			probe, err := ctor(cfg, log)
			if err != nil {
				// A config-requiring probe that the fixture table does not
				// cover: the invariant cannot inspect its EntitySource(). Add a
				// minimal valid-config fixture in probeConfigFixtures rather than
				// skipping — a skip is exactly the #482 blind spot (a probe could
				// ship with a nil/NoOp source and never be exercised here).
				t.Fatalf("probe %q failed to construct with its fixture: %v\n\n"+
					"add a minimal valid-config entry for %q to probeConfigFixtures "+
					"so the EntitySource invariant covers it (#482).", name, err, name)
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

// TestProbePackagesDoNotRegisterEntitySourcesDirectly locks in the #471
// unification: a probe declares its entity source via SetEntitySource in the
// constructor, and the ProbePoller alone registers EntitySource() with the
// detector (Start) and unregisters it (Shutdown). A probe package calling
// entity.RegisterSource itself reintroduces the dual mechanism this test
// exists to prevent — a runtime-registered source the EntitySource invariant
// never inspects (or, symmetrically, an inspected source that never reaches
// the detector).
//
// The check parses every non-test .go file in the probe subpackages
// (including files behind foreign-platform build tags, which a plain build
// would not reach) and fails on any selector call <entity-pkg>.RegisterSource.
func TestProbePackagesDoNotRegisterEntitySourcesDirectly(t *testing.T) {
	const entityPkgPath = "senhub-agent.go/internal/agent/services/entity"

	var offenders []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Only probe subpackages: the top-level files (probe_poller.go) hold
		// the one sanctioned entity.RegisterSource call site.
		if filepath.Dir(path) == "." {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		entityName := ""
		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil || p != entityPkgPath {
				continue
			}
			entityName = "entity"
			if imp.Name != nil {
				entityName = imp.Name.Name
			}
		}
		if entityName == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == entityName && sel.Sel.Name == "RegisterSource" {
				offenders = append(offenders, fmt.Sprintf("%s:%d", path, fset.Position(sel.Pos()).Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking probe packages: %v", err)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("probe packages must not call entity.RegisterSource directly — the "+
			"ProbePoller registers EntitySource() on Start and unregisters it on "+
			"Shutdown (#471). Declare the source with SetEntitySource in the "+
			"constructor instead. Offending call sites:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestListenerProbesImplementListenerHealth pins which probes must
// report real liveness. A probe whose Collect is a no-op has nothing to
// derive health from, so without ListenerHealth it reports healthy for
// the life of the agent whatever happened to its socket (#289). The
// list is explicit rather than derived: "does Collect return nil,nil"
// is not something a test can ask, and a new listener probe should have
// to add itself here deliberately.
func TestListenerProbesImplementListenerHealth(t *testing.T) {
	noOpCollectProbes := []string{"syslog", "event", "otlp_receiver"}

	for _, name := range noOpCollectProbes {
		t.Run(name, func(t *testing.T) {
			ctor, ok := probes.LookupProbeConstructor(name)
			if !ok {
				t.Fatalf("probe %q is not registered", name)
			}
			cfg := map[string]interface{}{"interval": 30}
			for k, v := range probeConfigFixtures[name] {
				cfg[k] = v
			}
			probe, err := ctor(cfg, logger.NewLogger(&cliArgs.ParsedArgs{}))
			if err != nil {
				t.Fatalf("constructing %q: %v", name, err)
			}
			if _, ok := probe.(types.ListenerProbe); !ok {
				t.Errorf("%q has a no-op Collect but does not implement types.ListenerProbe — it would report healthy with a dead listener", name)
			}
		})
	}
}

package snmppoll

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/hostiface"
	"senhub-agent.go/internal/agent/services/entity/macindex"
	"senhub-agent.go/internal/agent/services/governance"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpcore"
)

// Entity rail (#185, #156) — the polled device as a network.device entity, its
// IF-MIB ports as network.interface entities (has_interface) and its routing
// table as network.route entities (has_route, next hop a scalar next_hop.ip) —
// topology-as-entities, ADR 0022, frozen with Toise #222/#87. Wire shapes
// (network.device.id, network.interface.name, route.destination) are the Toise-frozen
// contract — see SNMP-OTEL-MAPPING.md Layer 2′; id-format decisions live in
// resolveDeviceID (lldp.go). LLDP adjacency is emitted as bare connected_to
// between the port entities. The legacy device-to-device edges (adjacent_to,
// routes_via, forwards_to) are gone — all replaced by the entity forms.

const (
	entityTypeNetworkDevice    = "network.device"
	entityTypeNetworkRoute     = "network.route"
	entityTypeNetworkInterface = "network.interface"
	entityTypeNetworkAddress   = "network.address"
	entityTypeNetworkNeighbor  = "network.neighbor"
	entityTypeHost             = "host"
	entityTypeServiceInstance  = "service.instance"
	idKeyServiceInstanceID     = "service.instance.id"
	idKeyNetworkDevice         = "network.device.id"
	idKeyHost                  = "host.id"
	idKeyRouteDestination      = "route.destination"
	idKeyInterfaceName         = "network.interface.name"
	idKeyNetworkAddress        = "network.address"
	idKeyNeighborMAC           = "mac"
	attrNextHopIP              = "next_hop.ip"
	attrRouteMetric            = "metric"
	// oper_state is a Toise state-key (closed set, underscore spelling): a change
	// fires entity.state_changed. The old dotted "oper.state" was silently
	// demoted to an ordinary attribute — see Toise #87 / the SNMP lot fix.
	attrOperState   = "oper_state"
	attrSpeed       = "speed"
	attrMac         = "mac"
	attrMtu         = "mtu"
	attrIfType      = "interface.type"
	attrDuplex      = "duplex"
	relConnectedTo  = "connected_to"
	relHasRoute     = "has_route"
	relHasInterface = "has_interface"
	relBoundTo      = "bound_to"
	relNextHopVia   = "next_hop_via"
	relMonitors     = "monitors"
	relRunsOn       = "runs_on"
	relNeighborOf   = "neighbor_of"
	relResolvesTo   = "resolves_to"
	attrBasis       = "basis"
	attrConfidence  = "confidence"

	resolveConfidenceARPNDP = 0.9
	resolveConfidenceFDB    = 0.7

	// Retired relation types — pre-ADR-0022 device-to-device edges that carried
	// their facts as edge attributes Toise's bare relationship descriptor drops.
	// Declared for vocabulary compat with the frozen contract (#239), NEVER
	// emitted: adjacent_to became connected_to between network.interface
	// entities, routes_via became network.route + has_route, and forwards_to
	// went away with the bridge-FDB walk (#156). Guarded by
	// TestBuildObservation_NoRetiredEdgeRelations.
	relRetiredAdjacentTo = "adjacent_to"
	relRetiredRoutesVia  = "routes_via"
	relRetiredForwardsTo = "forwards_to"

	// Polled-device descriptive attribute keys (network.device nameplate).
	attrSysDescr      = "sys.descr"
	attrHwVendor      = "hw.vendor"
	attrHwModel       = "hw.model"
	attrHwFirmwareVer = "hw.firmware_version"

	// Polled-device identity OIDs (dotted, no leading dot).
	oidEntPhysicalSerialNum   = "1.3.6.1.2.1.47.1.1.1.1.11" // ENTITY-MIB (per physical entity)
	oidEntPhysicalClass       = "1.3.6.1.2.1.47.1.1.1.1.5"  // entPhysicalClass (3 = chassis)
	oidEntPhysicalMfgName     = "1.3.6.1.2.1.47.1.1.1.1.12" // entPhysicalMfgName
	oidEntPhysicalModelName   = "1.3.6.1.2.1.47.1.1.1.1.13" // entPhysicalModelName
	oidEntPhysicalFirmwareRev = "1.3.6.1.2.1.47.1.1.1.1.9"  // entPhysicalFirmwareRev
	entPhysicalClassChassis   = 3
	oidSnmpEngineIDBase       = "1.3.6.1.6.3.10.2.1.1"
	oidSnmpEngineID           = "1.3.6.1.6.3.10.2.1.1.0" // SNMP-FRAMEWORK-MIB
	oidSysNameBase            = "1.3.6.1.2.1.1.5"
	oidSysName                = "1.3.6.1.2.1.1.5.0"
	oidSysDescrBase           = "1.3.6.1.2.1.1.1"
	oidSysDescr               = "1.3.6.1.2.1.1.1.0" // free-form vendor/model/OS line
	oidSysObjectIDBase        = "1.3.6.1.2.1.1.2"
	oidSysObjectID            = "1.3.6.1.2.1.1.2.0" // → vendor PEN
	oidSysServicesBase        = "1.3.6.1.2.1.1.7"
	oidSysServices            = "1.3.6.1.2.1.1.7.0" // OSI-layer bitmask → device.role
)

// snmpEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The SNMP topology walks run inside the probe's
// poll cycle (maybeSweep), rate-limited to topologyInterval so topology is
// swept far slower than metrics (Toise: ~5-15 min). The cache is re-emitted by
// the detector at its own faster cadence, so a device does not expire between
// sweeps.
type snmpEntitySource struct {
	cfg          *config
	interval     time.Duration
	moduleLogger *logger.ModuleLogger

	// discoveryInterval is the cadence of the discovery crawl when discovery
	// is configured; zero when it is not.
	discoveryInterval time.Duration

	mu sync.Mutex
	// cache is the last good sweep, kept raw: its neighbours are resolved
	// against the registry when the observation is read, so a device
	// polled before its neighbours links to them as soon as they are
	// polled, not one topology interval later.
	cache     sweepResult
	lastSweep time.Time
	// sweepFailures counts consecutive failed sweeps; retryAt is the
	// earliest next attempt after one, so a device that answers nothing
	// is not walked on every cycle.
	sweepFailures int
	retryAt       time.Time
	// swept marks that at least one sweep succeeded; before that the
	// empty cache is reported with ok=false.
	swept bool
	// failingSince is when sweeps started failing (zero while they succeed).
	// Past silentAfterSweeps topology intervals the cache stops being
	// replayed: it would keep asserting links of a device nobody can read.
	failingSince time.Time
	// unobserved holds what was dropped for that reason (the polled device's
	// cache, discovered devices aged out) until the detector collects it.
	unobserved   []entity.Entity
	silentMarked bool
	// deviceID + ifNames are the resolved identity of the polled device and its
	// ifIndex→ifName map, cached from the last sweep so the METRIC collector can
	// tag SNMP metrics with network.device.id / network.interface.name — the same
	// identity as the topology entities, so a backend joins device/interface
	// metrics to their entities. Replaced wholesale each sweep (never mutated in
	// place), so a reader holding the returned map sees a stable snapshot.
	deviceID string
	ifNames  map[string]string

	// discMu guards discObs, the latest discovery crawl observation. The
	// discovery loop rebuilds it each run and swaps it under this mutex.
	discMu  sync.Mutex
	discObs entity.Observation
	// discRan marks that a discovery run has completed: from then on an
	// empty discovery view means its devices left (as unobserved), not that
	// the view failed.
	discRan bool

	// registry reconciles a neighbour seen via LLDP with the canonical id of the
	// same device when another probe instance polls it directly. Shared across
	// instances in production; overridable in tests.
	registry *polledRegistry

	// now is the clock Observe reads; time.Now outside tests.
	now func() time.Time

	// hostID resolves the agent host's stable machine-id; nil → dbcommon.HostID.
	// When the SNMP target is the local host (loopback/localhost/empty) it anchors
	// the polled network.device to this host with a runs_on edge (#310).
	hostID func() string

	// hostInterfaceMACs returns this host's own interface MAC map; MAC → name.
	hostInterfaceMACs func() (map[string]string, error)

	// macIndex is the shared interface-MAC index used for resolves_to and
	// device-side network.neighbor emission. Defaults to the process-wide index.
	macIndex *macindex.Index
}

func newEntitySource(cfg *config, log *logger.ModuleLogger) *snmpEntitySource {
	iv := cfg.TopologyInterval
	if iv <= 0 {
		iv = defaultTopologyInterval
	}
	var discInterval time.Duration
	if cfg.Discovery != nil {
		discInterval = cfg.Discovery.Interval
		if discInterval <= 0 {
			discInterval = iv
		}
	}
	macindex.Default.SetTTL(3 * iv)
	return &snmpEntitySource{
		cfg:               cfg,
		interval:          iv,
		discoveryInterval: discInterval,
		moduleLogger:      log,
		registry:          sharedPolledRegistry,
		now:               time.Now,
		hostID:            dbcommon.HostID,
		hostInterfaceMACs: hostiface.OwnInterfaceMACs,
		macIndex:          macindex.Default,
	}
}

// Cadence implements entity.CadenceSource: the interval at which this source
// actually re-reads its facts. The topology sweep and discovery crawl are
// independent; the coarser one bounds the freshness of anything this source
// reports.
func (s *snmpEntitySource) Cadence() time.Duration {
	if s.discoveryInterval > s.interval {
		return s.discoveryInterval
	}
	return s.interval
}

// sweepResult is what one topology sweep read from the device.
type sweepResult struct {
	self          deviceIdentity
	topo          lldpTopology
	routes        []routeRow
	routesOmitted int
	ifaces        []ifaceRow
	addrs         []ipAddr
	deviceID      string
	ifNames       map[string]string
	fdb           []fdbEntry
	arp           []arpEntry
	lag           lagData
	// partial marks a sweep where at least one table walk failed: what is
	// missing from it was not read, not seen gone.
	partial bool
}

// Observe returns the last cached topology snapshot. Non-blocking; safe to
// call from the detector goroutine.
// Observe serves the cached snapshot. ok=false before the first
// successful sweep (nothing to report yet is not "everything deleted")
// — afterwards the cache always holds the last good sweep, because
// maybeSweep refuses to replace it with a failed one (audit D3).
//
// While the shared registry warms up after the agent starts, an
// observation with a neighbour the registry does not know yet reports
// ok=false: the neighbour is most likely a device another instance has
// not polled yet, and publishing without its link would tell the
// consumer the link is gone (relations ride their source entity, so an
// omitted one is a retracted one). Not publishing keeps the consumer's
// previous state alive until the neighbour is known.
func (s *snmpEntitySource) Observe() (entity.Observation, bool) {
	single, singleOK := s.observeSingle()
	disc, discRan := s.discoveryView()

	discEmpty := len(disc.Entities) == 0 && len(disc.Relations) == 0
	if s.silent() {
		// The device is reported through Unobserved; an empty single view
		// now removes it as unmonitored rather than holding it.
		singleOK = true
	}
	if !singleOK && discEmpty && !discRan {
		return entity.Observation{}, false
	}
	if !singleOK && discEmpty {
		// Discovery ran and every device it knew left: report the empty view.
		// ok=false here would make the detector replay the last one for up to
		// lastGoodTTL, keeping a cut link on the map for a quarter hour.
		return entity.Observation{}, true
	}
	obs := single
	if !(singleOK && discEmpty) {
		obs = mergeObservations(single, disc)
	}
	linkPrivateNextHops(&obs)
	// Applied again on the merged view: a discovered device's links were
	// built when it was collected, possibly before the far end was read
	// down in the same run.
	dropLinksToDownPorts(&obs, s.registry, s.now())
	annotatePeerState(&obs, s.registry, s.now())
	return obs, true
}

func (s *snmpEntitySource) observeSingle() (entity.Observation, bool) {
	s.mu.Lock()
	res, swept := s.cache, s.swept
	lastSweep := s.lastSweep
	replaying := !s.failingSince.IsZero()
	s.mu.Unlock()
	if !swept || s.silent() {
		return entity.Observation{}, false
	}
	now := s.now()
	obs, unresolved := s.build(res, now)
	if unresolved > 0 && s.registry != nil && s.registry.warming(now) {
		return entity.Observation{}, false
	}
	obs = s.appendResolvesNeighbors(obs, res)
	if replaying && !lastSweep.IsZero() {
		// The cache came from an earlier successful sweep. A consumer must
		// not see a fresh event_time on facts we have not re-read.
		stampObservedAt(&obs, lastSweep)
	}
	return obs, true
}

// silentAfterSweeps is how many topology intervals of failed sweeps the last
// good view is replayed for. It absorbs a lost SNMP reply without making the
// device leave and re-enter the graph; beyond it the replay would assert a
// topology no one has read.
const silentAfterSweeps = 3

// silent reports whether the polled device has failed long enough to stop
// being replayed. The first time it is, its last view is queued as
// unobserved so the detector removes it as unmonitored.
func (s *snmpEntitySource) silent() bool {
	now := s.now()
	s.mu.Lock()
	if s.failingSince.IsZero() || now.Sub(s.failingSince) < silentAfterSweeps*s.interval {
		s.mu.Unlock()
		return false
	}
	first := !s.silentMarked && s.swept
	s.silentMarked = true
	res := s.cache
	s.mu.Unlock()
	if first {
		obs, _ := s.build(res, now)
		s.markUnobserved(obs.Entities)
	}
	return true
}

// markUnobserved queues entities the source stopped reporting because it can
// no longer read them.
func (s *snmpEntitySource) markUnobserved(entities []entity.Entity) {
	if len(entities) == 0 {
		return
	}
	s.mu.Lock()
	s.unobserved = append(s.unobserved, entities...)
	s.mu.Unlock()
}

// Unobserved implements entity.UnobservedSource.
func (s *snmpEntitySource) Unobserved() []entity.Entity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.unobserved
	s.unobserved = nil
	return out
}

func (s *snmpEntitySource) setDiscoveryObservation(obs entity.Observation) {
	s.discMu.Lock()
	s.discObs = obs
	s.discRan = true
	s.discMu.Unlock()
}

func (s *snmpEntitySource) getDiscoveryObservation() entity.Observation {
	s.discMu.Lock()
	defer s.discMu.Unlock()
	return s.discObs
}

// build turns a sweep into the observation, resolving each LLDP neighbour
// against the registry as it stands now. unresolved counts the neighbours
// that advertise a chassis MAC no polled device has registered.
func (s *snmpEntitySource) build(res sweepResult, now time.Time) (entity.Observation, int) {
	unresolved := 0
	resolveNeighbor := func(n deviceIdentity) string {
		if s.registry != nil {
			if canon, ok := s.registry.canonicalFor(n, now); ok {
				return canon
			}
			if len(n.ChassisMAC) > 0 {
				unresolved++
			}
		}
		return resolveDeviceID(n)
	}
	obs := buildObservation(res.self, res.topo, res.routes, res.ifaces, res.addrs, resolveNeighbor)
	dropLinksToDownPorts(&obs, s.registry, now)
	maxRoutes := s.cfg.MaxRoutes
	if maxRoutes <= 0 {
		maxRoutes = defaultMaxRoutes
	}
	applyRouteCounters(&obs, res.deviceID, res.routesOmitted, maxRoutes)
	applyLag(&obs, res.lag, res.ifaces, res.deviceID)
	applyGovernance(obs, s.cfg, res.self, res.deviceID)
	s.appendLocalRunsOn(&obs, res.deviceID)
	return obs, unresolved
}

// DeviceID returns the resolved network.device.id of the polled device from the
// last sweep ("" before the first sweep). The metric collector tags every
// datapoint with it so device metrics join to the network.device entity.
func (s *snmpEntitySource) DeviceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceID
}

// InterfaceNames returns the ifIndex→ifName map from the last sweep (nil before
// the first sweep). The metric collector resolves an interface metric's
// if_index to network.interface.name so per-port metrics join to the network.interface
// entity. The returned map is a read-only snapshot (replaced, never mutated).
func (s *snmpEntitySource) InterfaceNames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ifNames
}

// maybeSweep refreshes the cached snapshot when topologyInterval has elapsed,
// reusing the probe's already-connected client. Called from Collect.
func (s *snmpEntitySource) maybeSweep(client snmpClient, now time.Time) {
	s.mu.Lock()
	due := (s.lastSweep.IsZero() || now.Sub(s.lastSweep) >= s.interval) && !now.Before(s.retryAt)
	s.mu.Unlock()
	if !due {
		return
	}

	bounded := newSweepClient(client, sweepBudget(s.cfg), time.Now)
	res, ok := s.sweep(bounded, now)
	if bounded.cut {
		s.moduleLogger.Warn().Str("target", s.cfg.Target).Dur("budget", sweepBudget(s.cfg)).
			Msg("topology sweep reached its budget; remaining walks skipped")
	}
	if !ok {
		// Identity unresolved (device unreachable or rejecting us):
		// keep the last good snapshot and do NOT stamp lastSweep, so
		// the retry does not wait a full topology interval while the
		// consumer's view decays (audit D3). The retry backs off while
		// failures repeat.
		s.mu.Lock()
		if s.failingSince.IsZero() {
			s.failingSince = now
		}
		s.sweepFailures++
		delay := sweepRetryDelay(s.cfg.Interval, s.sweepFailures)
		// Half a cycle early: the cycle that falls on retryAt starts a
		// little before or after it.
		s.retryAt = now.Add(delay - s.cfg.Interval/2)
		known := s.deviceID
		s.mu.Unlock()
		s.moduleLogger.Warn().Str("target", s.cfg.Target).Bool("silent", bounded.silent).Dur("retry_in", delay).
			Msg("topology sweep failed; keeping previous snapshot")
		if s.registry != nil {
			s.registry.recordUnreachable(known, s.interval, now)
		}
		return
	}

	s.mu.Lock()
	prev, hadPrev := s.cache, s.swept
	s.mu.Unlock()
	if res.partial && hadPrev {
		s.markUnobserved(droppedEntities(s.buildQuiet(prev, now), s.buildQuiet(res, now)))
	}

	s.mu.Lock()
	s.failingSince = time.Time{}
	s.silentMarked = false
	s.sweepFailures = 0
	s.retryAt = time.Time{}
	s.cache = res
	s.deviceID = res.deviceID
	s.ifNames = res.ifNames
	s.lastSweep = now
	s.swept = true
	s.mu.Unlock()
}

// sweep performs the SNMP reads. Best-effort: a failed LLDP walk still yields
// the polled device itself (identity from serial/engine/sysName, no
// neighbours). The result carries the resolved device id and the
// ifIndex→ifName map for the metric collector's correlation tags.
func (s *snmpEntitySource) sweep(client snmpClient, now time.Time) (sweepResult, bool) {
	partial := false
	topo, err := collectLLDP(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("LLDP walk failed; emitting device without neighbours")
		partial = true
		topo = lldpTopology{}
	}
	routes, err := collectRoutes(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("routing walk failed; emitting device without routes")
		partial = true
		routes = nil
	}
	ifaces, err := collectInterfaces(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("ifTable walk failed; emitting device without interfaces")
		partial = true
		ifaces = nil
	}
	addrs, err := collectIPAddrs(client)
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("ipAddrTable walk failed; emitting device without addresses")
		partial = true
		addrs = nil
	}
	self := readSelfIdentity(client, s.cfg.Target, topo.Local)

	deviceID := resolveDeviceID(self)
	var ifNames map[string]string
	if len(ifaces) > 0 {
		ifNames = make(map[string]string, len(ifaces))
		for _, ifc := range ifaces {
			if ifc.Name != "" {
				ifNames[ifc.Index] = ifc.Name
			}
		}
	}

	maxRoutes := s.cfg.MaxRoutes
	if maxRoutes <= 0 {
		maxRoutes = defaultMaxRoutes
	}
	routes, routesOmitted := selectRoutes(routes, self.MgmtIP, maxRoutes)
	if routesOmitted > 0 {
		s.moduleLogger.Info().
			Str("target", s.cfg.Target).
			Int("kept", len(routes)).
			Int("omitted", routesOmitted).
			Int("max_routes", maxRoutes).
			Msg("route table bound: some routes omitted")
	}

	var fdbs []fdbEntry
	var arps []arpEntry
	if idx := s.macIndex; idx != nil && idx.Enabled() {
		fdbs, err = collectFDB(client)
		if err != nil {
			s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
				Msg("bridge FDB walk failed; emitting device without neighbor resolution")
			fdbs = nil
		}
		arps, err = collectARP(client)
		if err != nil {
			s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
				Msg("ARP walk failed; emitting device without neighbor resolution")
			arps = nil
		}
	}

	// Register this directly-polled device's chassis MAC under its canonical id;
	// neighbours are resolved against the shared registry when the observation
	// is read (build): a neighbour known by the same MAC reconciles to the
	// canonical id another probe instance assigned it, instead of minting a
	// mac: shadow (one node per device, not several).
	if s.registry != nil {
		s.registry.recordPolled(self, deviceID, now)
		s.registry.recordPortStates(deviceID, ifaces, s.interval, now)
	}
	if idx := s.macIndex; idx != nil && idx.Enabled() && deviceID != "" {
		s.registerInterfaceMacs(deviceID, ifaces)
	}
	lag := collectLagData(client, ifaces, s.moduleLogger)
	// An unresolved identity means a failed sweep, not an empty network. So
	// does a device that answered nothing: the target address alone still
	// resolves to a mgmt: id, and caching that would replace the real device
	// with a neighbourless ghost, retracting its links as if they had been
	// seen to go.
	answered := deviceAnswered(self, topo, routes, ifaces, addrs)
	return sweepResult{self: self, topo: topo, routes: routes, routesOmitted: routesOmitted, ifaces: ifaces, addrs: addrs, deviceID: deviceID, ifNames: ifNames, fdb: fdbs, arp: arps, lag: lag, partial: partial}, deviceID != "" && answered
}

// deviceAnswered reports whether any read reached the device, as opposed to
// an identity built from the polled address alone.
func deviceAnswered(self deviceIdentity, topo lldpTopology, routes []routeRow, ifaces []ifaceRow, addrs []ipAddr) bool {
	return self.SysName != "" || self.Serial != "" || self.VendorPEN != "" || len(self.EngineID) > 0 ||
		len(self.ChassisMAC) > 0 || self.SysDescr != "" ||
		len(topo.Neighbors) > 0 || len(routes) > 0 || len(ifaces) > 0 || len(addrs) > 0
}

// appendLocalRunsOn anchors the polled device to the agent's own host with a
// `network.device --runs_on--> host` edge when the SNMP target is the local host
// with certainty (loopback / "localhost" / empty) — so an agent polling its own
// SNMP daemon hangs the device off the host node instead of floating (#310). A
// no-op for remote targets (a remote device must NOT claim to run on this host),
// when the device identity is unresolved, or when the host id is unknown.
//
// The edge is minted through the shared entity.LocalRunsOn helper so this
// emitter obeys the same collapse guard as every other local-anchor source: a
// device id that embeds the loopback target is byte-identical on every host and
// must not be anchored. Additionally, a loopback-DERIVED id ("mgmt:127.0.0.1",
// "name:localhost" — the last-resort rungs of resolveDeviceID) is refused
// regardless of the target literal: an SNMPv2c host with no serial/engine/MAC
// and a default sysName mints the same node on every host, the exact cross-host
// false-join the guard exists to prevent.
func (s *snmpEntitySource) appendLocalRunsOn(obs *entity.Observation, deviceID string) {
	if deviceID == "" || isLoopbackDerivedDeviceID(deviceID) {
		return
	}
	rel, ok := entity.LocalRunsOn(entityTypeNetworkDevice, deviceKey(deviceID), s.cfg.Target, s.hostID())
	if !ok {
		return
	}
	obs.Relations = append(obs.Relations, rel)
}

// isLoopbackDerivedDeviceID reports whether a resolved device id was minted from
// a loopback identifier — "mgmt:<loopback-ip>" or "name:localhost", the
// last-resort rungs of resolveDeviceID. Such an id is byte-identical on every
// host polling a default-configured local snmpd, so anchoring it to a host would
// false-join distinct hosts. The higher rungs (serial:/engine:/mac:) are
// globally unique and never loopback-derived.
func isLoopbackDerivedDeviceID(deviceID string) bool {
	if v, ok := strings.CutPrefix(deviceID, "mgmt:"); ok {
		return entity.IsLoopbackHost(v)
	}
	if v, ok := strings.CutPrefix(deviceID, "name:"); ok {
		return entity.IsLoopbackHost(strings.ToLower(strings.TrimSpace(v)))
	}
	return false
}

// applyGovernance stamps the operator governance attributes onto the polled
// (self) network.device entity: the single-target governance block plus any
// discovery rule that matches the device's facts (its poll IP, vendor and
// sysName), rules layered last. A no-op when nothing is configured or the device
// was not identified.
func applyGovernance(obs entity.Observation, cfg *config, self deviceIdentity, selfID string) {
	if selfID == "" {
		return
	}
	gov := map[string]any{}
	cfg.Governance.MergeInto(gov)
	if cfg.Discovery != nil && len(cfg.Discovery.GovernanceRules) > 0 {
		facts := governance.DeviceFacts{
			IP:      canonIP(self.MgmtIP),
			Vendor:  vendorName(self.VendorPEN),
			SysName: self.SysName,
		}
		for k, v := range cfg.Discovery.GovernanceRules.Apply(facts) {
			gov[k] = v
		}
	}
	if len(gov) == 0 {
		return
	}
	for i := range obs.Entities {
		if obs.Entities[i].Type == entityTypeNetworkDevice && obs.Entities[i].ID[idKeyNetworkDevice] == selfID {
			if obs.Entities[i].Attributes == nil {
				obs.Entities[i].Attributes = map[string]any{}
			}
			for k, v := range gov {
				obs.Entities[i].Attributes[k] = v
			}
			return
		}
	}
}

// readSelfIdentity reads the polled device's identifiers in the Toise-frozen
// precedence order. Each read is best-effort; resolveDeviceID degrades when a
// more stable id is absent.
func readSelfIdentity(client snmpClient, mgmtIP string, loc lldpLocal) deviceIdentity {
	di := deviceIdentity{MgmtIP: mgmtIP}

	// Chassis serial: take the serial of the single entPhysicalClass=chassis
	// row. A stack exposes N chassis rows — leave Serial empty so the id falls
	// to the stack-scoped snmpEngineID rather than a failover-unstable member
	// serial. Picking the chassis row (not "first serial") also avoids latching
	// onto a module/PSU serial that can change on a part swap.
	di.Serial = chassisSerial(client)
	// Vendor PEN namespaces the serial (serial is vendor-scoped, not global).
	if binds, err := client.WalkRaw(oidSysObjectIDBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysObjectID {
				di.VendorPEN = vendorPEN(asString(b.Value))
			}
		}
	}

	if binds, err := client.WalkRaw(oidSnmpEngineIDBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSnmpEngineID {
				di.EngineID = snmpcore.AsBytes(b.Value)
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysServicesBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysServices {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					di.Services = v
				}
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysNameBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysName {
				di.SysName = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
			}
		}
	}
	if binds, err := client.WalkRaw(oidSysDescrBase); err == nil {
		for _, b := range binds {
			if b.OID == oidSysDescr {
				di.SysDescr = strings.TrimSpace(snmpcore.OctetText(snmpcore.AsBytes(b.Value)))
			}
		}
	}
	di.HwVendor, di.HwModel, di.HwFirmware = chassisHardware(client)
	if di.SysName == "" {
		di.SysName = loc.SysName
	}
	if loc.ChassisIdSubtype == subtypeMacAddress {
		di.ChassisMAC = loc.ChassisId
	}
	return di
}

// chassisHardware reads the manufacturer / model / firmware nameplate of the
// device's single chassis (ENTITY-MIB entPhysical). Like chassisSerial it
// requires exactly one chassis row; a stack (>1 chassis) yields empty strings
// rather than latching onto an arbitrary member's nameplate. These are
// descriptive attributes only — never identity.
func chassisHardware(client snmpClient) (vendor, model, firmware string) {
	idx := singleChassisIndex(client)
	if idx == "" {
		return "", "", ""
	}
	read := func(base string) string {
		if binds, err := client.WalkRaw(base); err == nil {
			for _, b := range binds {
				if bIdx, ok := strings.CutPrefix(b.OID, base+"."); ok && bIdx == idx {
					return strings.TrimSpace(snmpcore.OctetText(snmpcore.AsBytes(b.Value)))
				}
			}
		}
		return ""
	}
	return read(oidEntPhysicalMfgName), read(oidEntPhysicalModelName), read(oidEntPhysicalFirmwareRev)
}

// singleChassisIndex returns the entPhysical index of the device's chassis when
// there is exactly one, or "" (no chassis, or a stack with several). Mirrors the
// chassis-selection rule chassisSerial uses for the device identity.
func singleChassisIndex(client snmpClient) string {
	var chassis []string
	if binds, err := client.WalkRaw(oidEntPhysicalClass); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalClass+"."); ok {
				if v, ok := snmpcore.AsInt(b.Value); ok && v == entPhysicalClassChassis {
					chassis = append(chassis, idx)
				}
			}
		}
	}
	if len(chassis) == 1 {
		return chassis[0]
	}
	return ""
}

// chassisSerial returns the serial of the device's single chassis, or "" when
// there is no chassis row, no serial, or MORE THAN ONE chassis (a stack —
// which is identified by its stack-wide snmpEngineID instead). It correlates
// entPhysicalClass and entPhysicalSerialNum by entPhysical index.
func chassisSerial(client snmpClient) string {
	class := map[string]int{}
	if binds, err := client.WalkRaw(oidEntPhysicalClass); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalClass+"."); ok {
				if v, ok := snmpcore.AsInt(b.Value); ok {
					class[idx] = v
				}
			}
		}
	}
	serial := map[string]string{}
	if binds, err := client.WalkRaw(oidEntPhysicalSerialNum); err == nil {
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, oidEntPhysicalSerialNum+"."); ok {
				if sn := strings.TrimSpace(snmpcore.OctetText(snmpcore.AsBytes(b.Value))); sn != "" {
					serial[idx] = sn
				}
			}
		}
	}

	var chassis []string
	for idx, cls := range class {
		if cls == entPhysicalClassChassis {
			if sn := serial[idx]; sn != "" {
				chassis = append(chassis, sn)
			}
		}
	}
	if len(chassis) == 1 {
		return chassis[0]
	}
	return "" // 0 → no usable serial; >1 → stack, use engineID
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// buildObservation maps the polled device, its interfaces, LLDP neighbours,
// routing table and bridge FDB to the frozen wire shapes:
//   - network.interface — one entity per named IF-MIB port the device owns
//     (has_interface), oper_state state-key + speed/mac/mtu/interface.type/
//     duplex descriptive; the port inventory that anchors connected_to;
//   - network.route — one entity per distinct destination CIDR the device owns
//     (has_route), next hop a scalar next_hop.ip;
//   - connected_to — bare port-to-port link adjacency from LLDP (supersedes the
//     legacy adjacent_to / forwards_to device-to-device edges, now retired).
//
// resolveNeighbor maps a discovered neighbour's identity to a network.device id.
// In production it consults the shared polled-device registry first (so a
// neighbour reconciles to the canonical id of the same device polled elsewhere)
// and falls back to resolveDeviceID; tests pass resolveDeviceID directly.
//
// Returns empty when the device cannot be identified (no usable id rung).
func buildObservation(self deviceIdentity, topo lldpTopology, routes []routeRow, ifaces []ifaceRow, addrs []ipAddr, resolveNeighbor func(deviceIdentity) string) entity.Observation {
	selfID := resolveDeviceID(self)
	if selfID == "" {
		return entity.Observation{}
	}

	obs := entity.Observation{}
	emitted := map[string]bool{}
	// scope carries the discovery method (#253): the polled device's own
	// identity + IF-MIB inventory ride snmp-ifmib; a neighbour exists only
	// because LLDP advertised it, so it rides snmp-lldp.
	addEntity := func(id string, attrs map[string]any, scope string) {
		if id == "" || emitted[id] {
			return
		}
		emitted[id] = true
		obs.Entities = append(obs.Entities, deviceEntity(id, attrs, scope))
	}
	addEntity(selfID, selfAttrs(self), entity.ScopeSNMPIFMIB)

	// monitors edge: agent → polled device. A configured target is anchored to
	// the agent's monitoring subgraph even when it exposes no named interface,
	// route or address, which would otherwise leave it with no relation and
	// have the anti-orphan guard drop it. Skipped without an agent id: the
	// consumer would buffer an unresolvable From, then drop it.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relMonitors,
			FromType: entityTypeServiceInstance, FromID: map[string]any{idKeyServiceInstanceID: agentID},
			ToType: entityTypeNetworkDevice, ToID: deviceKey(selfID),
		})
	}

	// network.interface — the device's ports as entities it owns. Bounded by
	// the device's port count; notPresent and unnamed rows are skipped, and a
	// duplicate network.interface.name keeps the first (identity is {device, name}).
	// Addresses per ifIndex with their mask, for the interface's
	// descriptive network attributes (ipAddrTable, IP-MIB).
	ifIPs := map[string][]string{}
	ifPrefixes := map[string]map[string]int{}
	for _, a := range addrs {
		ifIPs[a.IfIndex] = append(ifIPs[a.IfIndex], a.IP)
		if ifPrefixes[a.IfIndex] == nil {
			ifPrefixes[a.IfIndex] = map[string]int{}
		}
		ifPrefixes[a.IfIndex][a.IP] = a.Prefix
	}
	ifaceSeen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Name == "" || ifc.OperStatus == ifOperNotPresent || ifaceSeen[ifc.Name] {
			continue
		}
		ifaceSeen[ifc.Name] = true
		portID := map[string]any{idKeyNetworkDevice: selfID, idKeyInterfaceName: ifc.Name}
		attrs := map[string]any{attrOperState: operStateName(ifc.OperStatus)}
		if ifc.SpeedMbps > 0 {
			attrs[attrSpeed] = ifc.SpeedMbps * 1_000_000 // Mbit/s → bit/s
		}
		if ifc.Mac != "" {
			attrs[attrMac] = ifc.Mac
		}
		if ifc.Mtu > 0 {
			attrs[attrMtu] = ifc.Mtu
		}
		if t := ifTypeName(ifc.IfType); t != "" {
			attrs[attrIfType] = t
		}
		if d := duplexName(ifc.Duplex); d != "" {
			attrs[attrDuplex] = d
		}
		for k, v := range entity.InterfaceNetworkAttributes(ifIPs[ifc.Index], ifPrefixes[ifc.Index]) {
			attrs[k] = v
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeNetworkInterface, ID: portID, Attributes: attrs, Scope: entity.ScopeSNMPIFMIB,
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relHasInterface,
			FromType: entityTypeNetworkDevice, FromID: deviceKey(selfID),
			ToType: entityTypeNetworkInterface, ToID: portID,
		})
	}

	// network.interface.name lookups for connected_to: prefer the IF-MIB ifName so the
	// local port matches the network.interface entity emitted above (most gear
	// numbers lldpLocPortNum as ifIndex, so this hits); fall back to the LLDP
	// local-port table.
	ifOperByName := make(map[string]int, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Name != "" && ifc.OperStatus != 0 {
			ifOperByName[ifc.Name] = ifc.OperStatus
		}
	}
	ifIndexName := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		ifIndexName[ifc.Index] = ifc.Name
	}

	// connected_to — bare port-to-port link adjacency (supersedes adjacent_to).
	// Both endpoints are network.interface entities; the local one was emitted
	// above, the remote one by the neighbour's own poll. The neighbour device
	// is not built here: no relation targets a device, so the anti-orphan
	// guard dropped it on every cycle with a warning, and where the neighbour
	// is also polled its thin copy competed with the full one under the same
	// key. The link is skipped when either port cannot be named by exact
	// identity (no phantom port — point 7): an unnamed local port, an
	// unresolvable neighbour, or a MAC-only remote port.
	for _, n := range topo.Neighbors {
		nID := resolveNeighbor(neighborIdentity(n))
		if nID == "" || nID == selfID {
			continue
		}

		localIf := ifIndexName[n.LocalPortNum]
		if localIf == "" {
			localIf = topo.Local.Ports[n.LocalPortNum]
		}
		remoteIf := namedPortID(n.PortIdSubtype, n.PortId)
		if localIf == "" || remoteIf == "" {
			continue
		}
		// A device can keep a neighbour in its LLDP table until the entry's
		// TTL runs out after the link went down; a port that is not up
		// carries no link, so the adjacency is not asserted. An unread
		// status keeps the link.
		if st, ok := ifOperByName[localIf]; ok && st != ifOperUp {
			continue
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relConnectedTo,
			FromType: entityTypeNetworkInterface, FromID: interfacePortKey(selfID, localIf),
			ToType: entityTypeNetworkInterface, ToID: interfacePortKey(nID, remoteIf),
		})
	}

	// network.address — the device's interface IPs as entities, bound to their
	// interface. The SAME network.address {ip} node is referenced by a host's
	// next_hop_via when this device is that host's gateway, so the shared
	// address joins the host and device topology graphs. Keyed by IP alone, so
	// the IP MUST be globally unique: host-local / non-routable addresses
	// (loopback, link-local, wildcard, the Docker default-bridge gateway
	// 172.17.0.1 that exists on every host) are skipped — a shared node for them
	// would falsely link unrelated devices (network-derived-identity anti-pattern,
	// Toise otel-mapping contract). Skipped too when the interface cannot be named.
	addrSeen := map[string]bool{}
	for _, a := range addrs {
		ifName := ifIndexName[a.IfIndex]
		if ifName == "" || addrSeen[a.IP] ||
			!entity.AddressEdgeAllowed(a.IP) || entity.IsContainerBridgeIface(ifName) {
			continue
		}
		addrSeen[a.IP] = true
		addrID := map[string]any{idKeyNetworkAddress: a.IP}
		obs.Entities = append(obs.Entities, entity.Entity{Type: entityTypeNetworkAddress, ID: addrID, Scope: entity.ScopeSNMPIFMIB})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relBoundTo,
			FromType: entityTypeNetworkAddress, FromID: addrID,
			ToType: entityTypeNetworkInterface, ToID: interfacePortKey(selfID, ifName),
		})
	}

	// network.route — the polled device's routes as entities it owns. The
	// route emission lives in route_entities.go so the type/protocol/egress
	// and bound counter rules stay with the route vocabulary.
	appendRouteEntities(&obs, selfID, self, routes, ifaces, addrSeen)

	return obs
}

// registerInterfaceMacs records which interface of the device OWNS each MAC.
// An aggregate (ieee8023adLag) borrows a member's address, so it owns a MAC
// only when no physical interface carries it; when two physical interfaces
// carry the same MAC, ownership is undecidable and nothing is registered:
// a resolution must hold both identities, not one and a half.
func (s *snmpEntitySource) registerInterfaceMacs(deviceID string, ifaces []ifaceRow) {
	if s.macIndex == nil || deviceID == "" {
		return
	}
	for mac, name := range interfaceMACOwners(ifaces) {
		s.macIndex.Register(mac, deviceID, name)
	}
}

// interfaceMACOwners returns MAC → owning interface name for one device.
func interfaceMACOwners(ifaces []ifaceRow) map[string]string {
	physical := map[string][]string{}
	aggregate := map[string][]string{}
	for _, ifc := range ifaces {
		if ifc.Mac == "" || ifc.Name == "" {
			continue
		}
		if ifc.IfType == ifTypeIEEE8023adLag {
			aggregate[ifc.Mac] = append(aggregate[ifc.Mac], ifc.Name)
		} else {
			physical[ifc.Mac] = append(physical[ifc.Mac], ifc.Name)
		}
	}
	out := map[string]string{}
	for mac, names := range physical {
		if len(names) == 1 {
			out[mac] = names[0]
		}
	}
	for mac, names := range aggregate {
		if _, carried := physical[mac]; !carried && len(names) == 1 {
			out[mac] = names[0]
		}
	}
	return out
}

func (s *snmpEntitySource) appendResolvesNeighbors(obs entity.Observation, res sweepResult) entity.Observation {
	if s.macIndex == nil || !s.macIndex.Enabled() || res.deviceID == "" {
		return obs
	}

	hostMACs, err := s.hostInterfaceMACs()
	if err != nil {
		s.moduleLogger.Debug().Err(err).Str("target", s.cfg.Target).
			Msg("host interface MAC scan failed; skipping network.neighbor emission")
		return obs
	}
	if len(hostMACs) == 0 {
		return obs
	}

	hostID := s.hostID()
	if hostID == "" {
		return obs
	}

	ifName := func(ifIndex int) string {
		if res.ifNames == nil {
			return ""
		}
		return res.ifNames[strconv.Itoa(ifIndex)]
	}

	type heard struct {
		ifName     string
		hostIface  string
		basis      string
		confidence float64
	}
	groups := map[string][]heard{}
	add := func(mac string, ifIndex int, basis string, confidence float64) {
		canon, ok := entity.CanonicalMACString(mac)
		if !ok {
			return
		}
		hostIface, ok := hostMACs[canon]
		if !ok || hostIface == "" {
			return
		}
		port := ifName(ifIndex)
		if port == "" {
			return
		}
		groups[canon] = append(groups[canon], heard{
			ifName:     port,
			hostIface:  hostIface,
			basis:      basis,
			confidence: confidence,
		})
	}

	for _, e := range res.fdb {
		add(e.MAC, e.IfIndex, "fdb", resolveConfidenceFDB)
	}
	for _, e := range res.arp {
		add(e.MAC, e.IfIndex, "arp", resolveConfidenceARPNDP)
	}
	if len(groups) == 0 {
		return obs
	}

	macs := make([]string, 0, len(groups))
	for mac := range groups {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	for _, mac := range macs {
		entries := groups[mac]
		id := map[string]any{idKeyNetworkDevice: res.deviceID, idKeyNeighborMAC: mac}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:  entityTypeNetworkNeighbor,
			ID:    id,
			Scope: entity.ScopeSNMPIFMIB,
		})

		seenNeighborOf := map[string]bool{}
		seenResolves := map[string]bool{}
		for _, e := range entries {
			if !seenNeighborOf[e.ifName] {
				seenNeighborOf[e.ifName] = true
				obs.Relations = append(obs.Relations, entity.Relation{
					Type:     relNeighborOf,
					FromType: entityTypeNetworkNeighbor,
					FromID:   id,
					ToType:   entityTypeNetworkInterface,
					ToID:     interfacePortKey(res.deviceID, e.ifName),
				})
			}

			key := e.basis + "\x00" + e.hostIface
			if !seenResolves[key] {
				seenResolves[key] = true
				obs.Relations = append(obs.Relations, entity.Relation{
					Type:     relResolvesTo,
					FromType: entityTypeNetworkNeighbor,
					FromID:   id,
					ToType:   entityTypeNetworkInterface,
					ToID:     map[string]any{idKeyHost: hostID, idKeyInterfaceName: e.hostIface},
					Attributes: map[string]any{
						attrBasis:      e.basis,
						attrConfidence: e.confidence,
					},
				})
			}
		}
	}
	return obs
}

func deviceEntity(id string, attrs map[string]any, scope string) entity.Entity {
	return entity.Entity{Type: entityTypeNetworkDevice, ID: deviceKey(id), Attributes: attrs, Scope: scope}
}

func deviceKey(id string) map[string]any {
	return map[string]any{idKeyNetworkDevice: id}
}

// interfacePortKey is the exact identity of a network.interface entity: its
// owning device plus the port name.
func interfacePortKey(deviceID, ifName string) map[string]any {
	return map[string]any{idKeyNetworkDevice: deviceID, idKeyInterfaceName: ifName}
}

// selfAttrs carries only observer-independent descriptive
// attributes (ENTITY-DETECTION.md §6b): the same device seen by two agents
// must not flap on last-writer-wins. The address this agent polled is not
// one of them: three agents reaching a router through three of its
// addresses rewrote it in turn on every report. The device's addresses are
// published as network.address entities bound to its interfaces.
func selfAttrs(self deviceIdentity) map[string]any {
	attrs := map[string]any{}
	add := func(k, v string) {
		if v != "" {
			attrs[k] = v
		}
	}
	add("sys.name", self.SysName)
	add(attrSysDescr, self.SysDescr)
	add("device.role", deviceRole(self.Services))
	add("vendor", vendorName(self.VendorPEN))
	add(attrHwVendor, self.HwVendor)
	add(attrHwModel, self.HwModel)
	add(attrHwFirmwareVer, self.HwFirmware)
	return attrs
}

// deviceRole infers a readable role from the IF-MIB-adjacent sysServices
// bitmask (sum of 2^(layer-1) over the OSI layers the device offers): a device
// that forwards at layer 3 is a router, one that only bridges at layer 2 is a
// switch. Returns "" when sysServices is unread or offers neither.
func deviceRole(services int) string {
	switch {
	case services&0x04 != 0: // layer 3 (internet) → forwards/routes
		return "router"
	case services&0x02 != 0: // layer 2 (datalink) only → bridges/switches
		return "switch"
	default:
		return ""
	}
}

// vendorName maps a vendor's IANA Private Enterprise Number to a readable name
// for the common network vendors; an unknown PEN returns "" (the PEN already
// rides in the serial:<PEN>:… identity, so it is never lost).
func vendorName(pen string) string {
	switch pen {
	case "9":
		return "cisco"
	case "2636":
		return "juniper"
	case "30065":
		return "arista"
	case "2011":
		return "huawei"
	case "11":
		return "hp"
	case "674":
		return "dell"
	case "6027":
		return "dell-force10"
	case "25506":
		return "h3c"
	case "1916":
		return "extreme"
	case "14988":
		return "mikrotik"
	case "8072":
		return "net-snmp"
	default:
		return ""
	}
}

func (s *snmpEntitySource) buildQuiet(res sweepResult, now time.Time) entity.Observation {
	obs, _ := s.build(res, now)
	return obs
}

// stampObservedAt sets ObservedAt on entities that do not already carry an
// older observation time.
func stampObservedAt(obs *entity.Observation, at time.Time) {
	for i := range obs.Entities {
		if obs.Entities[i].ObservedAt.IsZero() {
			obs.Entities[i].ObservedAt = at
		}
	}
}

// droppedEntities returns the entities of before that after no longer holds.
// After a partial sweep these are what the failed walk could not read, so
// they leave as unmonitored rather than as seen gone.
func droppedEntities(before, after entity.Observation) []entity.Entity {
	kept := make(map[string]bool, len(after.Entities))
	for _, e := range after.Entities {
		kept[entityIdentity(e)] = true
	}
	var out []entity.Entity
	for _, e := range before.Entities {
		if !kept[entityIdentity(e)] {
			out = append(out, e)
		}
	}
	return out
}

func entityIdentity(e entity.Entity) string {
	keys := make([]string, 0, len(e.ID))
	for k := range e.ID {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(e.Type)
	for _, k := range keys {
		fmt.Fprintf(&b, "|%s=%v", k, e.ID[k])
	}
	return b.String()
}

// DepartureReason implements entity.DepartureSource: a device's neighbours
// come from its ARP and bridge tables, caches that forget idle entries, so a
// neighbour that stops being listed aged out, not seen to leave. Devices,
// interfaces and routes keep the default.
func (s *snmpEntitySource) DepartureReason(e entity.Entity) string {
	if e.Type == entityTypeNetworkNeighbor {
		return entity.ReasonAgedOut
	}
	return ""
}

var (
	_ entity.CadenceSource    = (*snmpEntitySource)(nil)
	_ entity.DepartureSource  = (*snmpEntitySource)(nil)
	_ entity.UnobservedSource = (*snmpEntitySource)(nil)
)

func (s *snmpEntitySource) discoveryView() (entity.Observation, bool) {
	s.discMu.Lock()
	defer s.discMu.Unlock()
	return s.discObs, s.discRan
}

package snmppoll

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/governance"
	"senhub-agent.go/internal/agent/tags"
)

const (
	discoveryMaxConcurrent = 4
	// discoveryMissGrace is how many consecutive runs a known device may
	// fail to answer before it leaves the observation: one lost UDP
	// exchange must not retract a switch and all its links.
	discoveryMissGrace = 3

	metricDiscoveryDevices             = "snmp_poll_discovery_devices"
	metricDiscoveryRuns                = "snmp_poll_discovery_runs"
	metricDiscoveryLastDurationSeconds = "snmp_poll_discovery_last_duration_seconds"
	metricDiscoveryFailures            = "snmp_poll_discovery_failures"
	metricDiscoverySeeds               = "snmp_poll_discovery_seeds"
)

type discoveryMetrics struct {
	mu                  sync.Mutex
	devices             int
	runs                uint64
	lastDuration        time.Duration
	failuresUnreachable uint64
	failuresIdentity    uint64
	seedsConfigured     int
	seedsHost           int
}

func (m *discoveryMetrics) record(devices int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices = devices
	m.runs++
	m.lastDuration = d
}

func (m *discoveryMetrics) recordSeeds(configured, host int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seedsConfigured = configured
	m.seedsHost = host
}

func (m *discoveryMetrics) addFailure(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch reason {
	case "unreachable":
		m.failuresUnreachable++
	case "identity":
		m.failuresIdentity++
	}
}

func (m *discoveryMetrics) snapshot() (devices int, runs uint64, last time.Duration, unreachable, identity uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.devices, m.runs, m.lastDuration, m.failuresUnreachable, m.failuresIdentity
}

func (m *discoveryMetrics) snapshotSeeds() (configured, host int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seedsConfigured, m.seedsHost
}

// discoveredState is the last good observation of one discovered device
// and how many runs in a row it has been missing since.
type discoveredState struct {
	obs    entity.Observation
	at     time.Time
	misses int
}

// discoveredTopologyResult is one polled device's observation plus whether
// the poll was partial: at least one table walk failed, so missing entities
// were not read rather than seen gone.
type discoveredTopologyResult struct {
	obs     entity.Observation
	partial bool
}

func (p *snmppollProbe) discoveryInterval() time.Duration {
	if p.cfg.Discovery != nil && p.cfg.Discovery.Interval > 0 {
		return p.cfg.Discovery.Interval
	}
	if p.cfg.TopologyInterval > 0 {
		return p.cfg.TopologyInterval
	}
	return defaultTopologyInterval
}

func (p *snmppollProbe) discoveryLoop(ctx context.Context) {
	defer p.discoveryWG.Done()

	// First run right after start.
	p.runDiscovery(ctx)

	interval := p.discoveryInterval()
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		p.runDiscovery(ctx)
	}
}

func (p *snmppollProbe) runDiscovery(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	start := time.Now()
	dcfg := p.cfg.Discovery

	allowed := dcfg.AllowedCIDRs
	var hostGateways []string
	hostScope := p.hostScope
	if hostScope == nil {
		hostScope = defaultHostScope
	}
	if dcfg.SeedFromHost || dcfg.AllowedFromHost {
		// A partial read still counts: a platform without a route reader
		// has no gateway but still bounds the crawl to its own networks.
		gws, nets, err := hostScope()
		if err != nil {
			p.hostScopeErrStreak++
			if p.hostScopeErrStreak == 1 {
				p.moduleLogger.Warn().Err(err).Int("gateways", len(gws)).Int("networks", len(nets)).
					Msg("discovery: host scope partly unreadable; using what was read and the configured seeds")
			}
		} else {
			p.hostScopeErrStreak = 0
		}
		hostGateways = gws
		if dcfg.AllowedFromHost {
			allowed = nets
		}
	}

	configuredSeeds := dedupeStrings(dcfg.Seeds)
	hostSeeds := []string{}
	if dcfg.SeedFromHost {
		for _, gw := range hostGateways {
			if inAllowedCIDR(gw, allowed) {
				hostSeeds = append(hostSeeds, gw)
			}
		}
		hostSeeds = dedupeStrings(hostSeeds)
	}

	seeds := append([]string{}, configuredSeeds...)
	seenSeeds := map[string]bool{}
	for _, s := range configuredSeeds {
		seenSeeds[s] = true
	}
	hostSeedsUsed := 0
	for _, s := range hostSeeds {
		if !seenSeeds[s] {
			seenSeeds[s] = true
			seeds = append(seeds, s)
			hostSeedsUsed++
		}
	}
	p.discoveryMet.recordSeeds(len(configuredSeeds), hostSeedsUsed)

	if len(seeds) == 0 {
		p.noSeedStreak++
		if p.noSeedStreak == 1 {
			p.moduleLogger.Info().Msg("no seed for this run")
		}

		// Keep the previous observation retention logic unchanged: an empty
		// crawl ages known devices out after the usual miss grace.
		obs := p.retainRecentlySeen(nil)
		if p.entitySource != nil {
			p.entitySource.setDiscoveryObservation(obs)
		}

		elapsed := time.Since(start)
		p.discoveryMet.record(0, elapsed)

		if elapsed > p.discoveryInterval() {
			p.moduleLogger.Warn().
				Dur("elapsed", elapsed).
				Dur("interval", p.discoveryInterval()).
				Msg("discovery run exceeded interval; next run starts after a full interval")
		}

		p.moduleLogger.Info().
			Int("devices", 0).
			Int("devices_kept", len(p.discovered)).
			Dur("duration", elapsed).
			Msg("discovery run completed")
		return
	}
	p.noSeedStreak = 0

	bounds := crawlBounds{MaxDevices: dcfg.MaxDevices, MaxHops: dcfg.MaxHops, Allowed: allowed}

	poll := func(target string) (string, []string) {
		return p.crawlPollDevice(ctx, target)
	}
	discovered := crawlWithWorkers(seeds, poll, bounds, discoveryMaxConcurrent)
	discovered = p.filterOwnTarget(discovered)

	fresh := p.collectDiscoveredTopology(ctx, discovered)
	obs := p.retainRecentlySeen(fresh)
	if p.entitySource != nil {
		p.entitySource.setDiscoveryObservation(obs)
	}

	elapsed := time.Since(start)
	answered := len(fresh)
	p.discoveryMet.record(answered, elapsed)

	if elapsed > p.discoveryInterval() {
		p.moduleLogger.Warn().
			Dur("elapsed", elapsed).
			Dur("interval", p.discoveryInterval()).
			Msg("discovery run exceeded interval; next run starts after a full interval")
	}

	p.moduleLogger.Info().
		Int("devices", answered).
		Int("devices_kept", len(p.discovered)).
		Dur("duration", elapsed).
		Msg("discovery run completed")
}

func (p *snmppollProbe) crawlPollDevice(ctx context.Context, target string) (string, []string) {
	if ctx.Err() != nil {
		return "", nil
	}

	client := p.newClient(p.discoveryClientConfig(target))
	if err := client.Connect(); err != nil {
		p.discoveryMet.addFailure("unreachable")
		p.moduleLogger.Warn().Err(err).Str("target", target).Msg("discovery: SNMP connect failed")
		return "", nil
	}
	defer func() {
		if err := client.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Str("target", target).Msg("discovery: SNMP close failed")
		}
	}()

	// sysObjectID is mandatory SNMPv2-MIB, so a device that does not answer it
	// is treated as unreachable rather than discovered by mgmt-IP fallback.
	if _, err := client.WalkRaw(oidSysObjectIDBase); err != nil {
		p.discoveryMet.addFailure("unreachable")
		p.moduleLogger.Warn().Err(err).Str("target", target).Msg("discovery: sysObjectID walk failed")
		return "", nil
	}

	topo, err := collectLLDP(client)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("target", target).Msg("discovery: LLDP walk failed; treating as no neighbours")
		topo = lldpTopology{}
	}

	self := readSelfIdentity(client, target, topo.Local)
	deviceID := resolveDeviceID(self)
	if deviceID == "" {
		p.discoveryMet.addFailure("identity")
		p.moduleLogger.Warn().Str("target", target).Msg("discovery: unable to resolve device identity")
		return "", nil
	}

	if p.entitySource != nil && p.entitySource.registry != nil {
		p.entitySource.registry.recordPolled(self, deviceID, time.Now())
	}

	neighbours := make([]string, 0, len(topo.Neighbors))
	for _, n := range topo.Neighbors {
		if n.MgmtIP != "" {
			neighbours = append(neighbours, n.MgmtIP)
		}
	}
	return deviceID, neighbours
}

func (p *snmppollProbe) filterOwnTarget(devices []discoveredDevice) []discoveredDevice {
	singleID := ""
	if p.entitySource != nil {
		singleID = p.entitySource.DeviceID()
	}
	out := devices[:0]
	for _, d := range devices {
		if singleID != "" && d.ID == singleID {
			continue
		}
		if canonIP(d.MgmtIP) == canonIP(p.cfg.Target) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (p *snmppollProbe) discoveryClientConfig(target string) *config {
	c := *p.cfg
	c.Target = target
	c.Version = gosnmp.Version2c
	c.Community = p.cfg.Discovery.Profile.Community
	c.V3 = nil
	return &c
}

// retainRecentlySeen merges this run's per-device observations with the
// devices that answered in one of the last discoveryMissGrace runs.
// Only the discovery goroutine calls it, so p.discovered needs no lock.
func (p *snmppollProbe) retainRecentlySeen(fresh map[string]discoveredTopologyResult) entity.Observation {
	if p.discovered == nil {
		p.discovered = map[string]*discoveredState{}
	}
	for id, res := range fresh {
		if prev, ok := p.discovered[id]; ok && res.partial && p.entitySource != nil {
			// A partial poll could not read some tables, so entities missing
			// from it left unobserved, not removed as if they were seen gone.
			p.entitySource.markUnobserved(droppedEntities(prev.obs, res.obs))
		}
		p.discovered[id] = &discoveredState{obs: res.obs, at: time.Now()}
	}
	ids := make([]string, 0, len(p.discovered))
	for id, st := range p.discovered {
		if _, ok := fresh[id]; !ok {
			st.misses++
			if p.entitySource != nil && p.entitySource.registry != nil {
				p.entitySource.registry.recordUnreachable(id, p.discoveryInterval(), time.Now())
			}
			if st.misses >= discoveryMissGrace {
				if p.entitySource != nil {
					p.entitySource.markUnobserved(st.obs.Entities)
					p.entitySource.forgetDiscoveredL2(id)
				}
				delete(p.discovered, id)
				continue
			}
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var merged entity.Observation
	for _, id := range ids {
		obs := p.discovered[id].obs
		if _, ok := fresh[id]; !ok {
			at := p.discovered[id].at
			if at.IsZero() {
				at = time.Now()
				p.discovered[id].at = at
			}
			obs = copyObservationWithObservedAt(obs, at)
		}
		merged = mergeObservations(merged, obs)
	}
	return merged
}

func copyObservationWithObservedAt(obs entity.Observation, at time.Time) entity.Observation {
	out := obs
	out.Entities = make([]entity.Entity, len(obs.Entities))
	copy(out.Entities, obs.Entities)
	stampObservedAt(&out, at)
	return out
}

// collectDiscoveredTopology polls each device and returns its result
// keyed by device id; a device that fails this run is absent from the map.
func (p *snmppollProbe) collectDiscoveredTopology(ctx context.Context, devices []discoveredDevice) map[string]discoveredTopologyResult {
	out := map[string]discoveredTopologyResult{}
	if len(devices) == 0 {
		return out
	}

	results := make([]discoveredTopologyResult, len(devices))
	var wg sync.WaitGroup
	sem := make(chan struct{}, discoveryMaxConcurrent)

	for i, d := range devices {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, d discoveredDevice) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = p.pollDiscoveredDevice(ctx, d)
		}(i, d)
	}
	wg.Wait()

	for i, res := range results {
		if len(res.obs.Entities) > 0 {
			out[devices[i].ID] = res
		}
	}
	return out
}

func (p *snmppollProbe) pollDiscoveredDevice(ctx context.Context, d discoveredDevice) discoveredTopologyResult {
	if ctx.Err() != nil {
		return discoveredTopologyResult{}
	}

	client := p.newClient(p.discoveryClientConfig(d.MgmtIP))
	if err := client.Connect(); err != nil {
		p.discoveryMet.addFailure("unreachable")
		p.moduleLogger.Warn().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: SNMP connect failed")
		return discoveredTopologyResult{}
	}
	defer func() {
		if err := client.Close(); err != nil {
			p.moduleLogger.Warn().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: SNMP close failed")
		}
	}()

	partial := false

	topo, err := collectLLDP(client)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: LLDP walk failed")
		topo = lldpTopology{}
		partial = true
	}
	routes, err := collectRoutes(client)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: routing walk failed")
		routes = nil
		partial = true
	}
	ifaces, skippedWalks, err := collectInterfaces(client)
	if len(skippedWalks) > 0 {
		p.moduleLogger.Debug().Str("target", d.MgmtIP).Strs("skipped_walks", skippedWalks).Msg("discovery topology: optional ifTable walks failed")
	}
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: ifTable walk failed")
		ifaces = nil
		partial = true
	}
	addrs, err := collectIPAddrs(client)
	if err != nil {
		p.moduleLogger.Debug().Err(err).Str("target", d.MgmtIP).Msg("discovery topology: ipAddrTable walk failed")
		addrs = nil
		partial = true
	}

	self := readSelfIdentity(client, d.MgmtIP, topo.Local)
	deviceID := resolveDeviceID(self)
	if deviceID == "" {
		p.discoveryMet.addFailure("identity")
		p.moduleLogger.Warn().Str("target", d.MgmtIP).Msg("discovery topology: unable to resolve device identity")
		return discoveredTopologyResult{}
	}
	if deviceID != d.ID {
		p.moduleLogger.Warn().
			Str("target", d.MgmtIP).
			Str("crawl_id", d.ID).
			Str("topology_id", deviceID).
			Msg("discovery topology: identity changed between crawl and topology collection")
		return discoveredTopologyResult{}
	}

	if p.entitySource != nil && p.entitySource.registry != nil {
		p.entitySource.registry.recordPolled(self, deviceID, time.Now())
		p.entitySource.registry.recordPortStates(deviceID, ifaces, p.discoveryInterval(), time.Now())
	}

	resolveNeighbor := func(n deviceIdentity) string {
		if p.entitySource != nil && p.entitySource.registry != nil {
			if canon, ok := p.entitySource.registry.canonicalFor(n, time.Now()); ok {
				return canon
			}
		}
		return resolveDeviceID(n)
	}

	maxRoutes := p.cfg.MaxRoutes
	if maxRoutes <= 0 {
		maxRoutes = defaultMaxRoutes
	}
	routes, routesOmitted := selectRoutes(routes, self.MgmtIP, maxRoutes)
	obs := buildObservation(self, topo, routes, ifaces, addrs, resolveNeighbor)
	if p.entitySource != nil {
		dropLinksToDownPorts(&obs, p.entitySource.registry, time.Now())
	}
	applyRouteCounters(&obs, deviceID, routesOmitted, maxRoutes)
	applyLag(&obs, collectLagData(client, ifaces, p.moduleLogger), ifaces, deviceID)
	applyDiscoveredGovernance(&obs, p.cfg.Discovery.GovernanceRules, self, deviceID)
	// The gateway and the access switch are usually discovered, not the
	// polled target: the MAC resolution must cover them too.
	if es := p.entitySource; es != nil {
		if es.macIndex != nil && es.macIndex.Enabled() {
			es.registerInterfaceMacs(deviceID, ifaces)
		}
		if es.maxNeighbors() > 0 {
			res := sweepResult{deviceID: deviceID, ifNames: ifaceNameMap(ifaces), ifaces: ifaces}
			res.fdb, res.arp = es.discoveredL2Rows(deviceID, client)
			obs = es.appendResolvesNeighbors(obs, res)
		}
	}
	return discoveredTopologyResult{obs: obs, partial: partial}
}

// ifaceNameMap is the ifIndex → ifName map of a set of interface rows.
func ifaceNameMap(ifaces []ifaceRow) map[string]string {
	out := make(map[string]string, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Name != "" {
			out[ifc.Index] = ifc.Name
		}
	}
	return out
}

func applyDiscoveredGovernance(obs *entity.Observation, rules governance.Rules, self deviceIdentity, selfID string) {
	if selfID == "" || len(rules) == 0 {
		return
	}
	gov := rules.Apply(governance.DeviceFacts{
		IP:      canonIP(self.MgmtIP),
		Vendor:  vendorName(self.VendorPEN),
		SysName: self.SysName,
	})
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

func (p *snmppollProbe) discoveryDataPoints(now time.Time) []data_store.DataPoint {
	devices, runs, last, unreachable, identity := p.discoveryMet.snapshot()
	configuredSeeds, hostSeeds := p.discoveryMet.snapshotSeeds()
	baseTags := discoveryMetricTags(p.instance)

	return []data_store.DataPoint{
		{Name: metricDiscoveryDevices, Timestamp: now, Value: float64(devices), Tags: baseTags},
		{Name: metricDiscoveryRuns, Timestamp: now, Value: float64(runs), Tags: baseTags},
		{Name: metricDiscoveryLastDurationSeconds, Timestamp: now, Value: last.Seconds(), Tags: baseTags},
		{Name: metricDiscoveryFailures, Timestamp: now, Value: float64(unreachable), Tags: append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "reason", Value: "unreachable"})},
		{Name: metricDiscoveryFailures, Timestamp: now, Value: float64(identity), Tags: append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "reason", Value: "identity"})},
		{Name: metricDiscoverySeeds, Timestamp: now, Value: float64(configuredSeeds), Tags: append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "source", Value: "configured"})},
		{Name: metricDiscoverySeeds, Timestamp: now, Value: float64(hostSeeds), Tags: append(append([]tags.Tag{}, baseTags...), tags.Tag{Key: "source", Value: "host"})},
	}
}

func discoveryMetricTags(instance string) []tags.Tag {
	return statusTags(instance)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// crawlWithWorkers is a worker-pool BFS crawl preserving crawl's deterministic
// BFS order: items are polled concurrently in batches of at most workers and
// their results are expanded in input order.
func crawlWithWorkers(seeds []string, poll crawlPoller, b crawlBounds, workers int) []discoveredDevice {
	if workers <= 0 {
		workers = 1
	}
	type item struct {
		ip  string
		hop int
	}
	queue := make([]item, 0, len(seeds))
	for _, s := range seeds {
		queue = append(queue, item{ip: s, hop: 0})
	}

	visitedIP := map[string]bool{}
	seenDev := map[string]bool{}
	var out []discoveredDevice

	for len(queue) > 0 && len(out) < b.MaxDevices {
		batchSize := workers
		if remaining := b.MaxDevices - len(out); remaining < batchSize {
			batchSize = remaining
		}

		var batch []item
		for len(queue) > 0 && len(batch) < batchSize {
			it := queue[0]
			queue = queue[1:]
			if visitedIP[it.ip] {
				continue
			}
			visitedIP[it.ip] = true
			batch = append(batch, it)
		}
		if len(batch) == 0 {
			break
		}

		type pollResult struct {
			it          item
			deviceID    string
			neighborIPs []string
		}
		results := make([]pollResult, len(batch))
		var wg sync.WaitGroup
		for i, it := range batch {
			wg.Add(1)
			go func(i int, it item) {
				defer wg.Done()
				deviceID, neighborIPs := poll(it.ip)
				results[i] = pollResult{it: it, deviceID: deviceID, neighborIPs: neighborIPs}
			}(i, it)
		}
		wg.Wait()

		for _, r := range results {
			if r.deviceID == "" {
				continue
			}
			if seenDev[r.deviceID] {
				continue
			}
			seenDev[r.deviceID] = true
			out = append(out, discoveredDevice{ID: r.deviceID, MgmtIP: r.it.ip, Hop: r.it.hop})

			if r.it.hop >= b.MaxHops {
				continue
			}
			for _, nip := range r.neighborIPs {
				if nip != "" && !visitedIP[nip] && inAllowedCIDR(nip, b.Allowed) {
					queue = append(queue, item{ip: nip, hop: r.it.hop + 1})
				}
			}
		}
	}
	return out
}

// mergeObservations concatenates observations, deduplicating entities and
// relations by exact identity.
func mergeObservations(observations ...entity.Observation) entity.Observation {
	var out entity.Observation
	seenEntities := map[string]bool{}
	seenRelations := map[string]bool{}
	for _, obs := range observations {
		for _, e := range obs.Entities {
			k := entityMergeKey(e)
			if !seenEntities[k] {
				seenEntities[k] = true
				out.Entities = append(out.Entities, e)
			}
		}
		for _, r := range obs.Relations {
			k := relationMergeKey(r)
			if !seenRelations[k] {
				seenRelations[k] = true
				out.Relations = append(out.Relations, r)
			}
		}
	}
	return out
}

func entityMergeKey(e entity.Entity) string {
	return e.Type + "|" + idMergeKey(e.ID)
}

func relationMergeKey(r entity.Relation) string {
	return r.Type + "|" + r.FromType + "|" + idMergeKey(r.FromID) + "|" + r.ToType + "|" + idMergeKey(r.ToID)
}

func idMergeKey(id map[string]any) string {
	if len(id) == 0 {
		return ""
	}
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", id[k])
		b.WriteByte(';')
	}
	return b.String()
}

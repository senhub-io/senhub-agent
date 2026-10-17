package snmppoll

import (
	"context"
	"fmt"
	"net"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"strconv"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpmib"
	"senhub-agent.go/internal/agent/tags"
)

const keysWaitCycles = 3

// snmppollProbe polls one SNMP target each cycle. It feeds two rails: the
// metric rail (built-in MIB modules + custom OID mappings → datapoints)
// and, from Lot 5, the entity rail (topology → entity.Source). Lot 1b
// implements MIB-II + IF-MIB on the metric rail and scaffolds the entity
// source. See docs/developer-guide/engineering/SNMP-OTEL-MAPPING.md.
type snmppollProbe struct {
	*types.BaseProbe
	cfg          *config
	instance     string
	moduleLogger *logger.ModuleLogger
	entitySource *snmpEntitySource

	// newClient is the SNMP client factory, overridable in tests.
	newClient func(*config) snmpClient

	// hostScope supplies the host's default gateways and private connected
	// networks for per-run discovery bounds. Overridable in tests.
	hostScope func() (gateways []string, networks []*net.IPNet, err error)

	// Discovery background loop lifecycle. discoveryCancel cancels the loop
	// started by OnStart; discoveryWG waits for it in OnShutdown.
	discoveryCancel    context.CancelFunc
	discoveryWG        sync.WaitGroup
	discoveryMet       discoveryMetrics
	discovered         map[string]*discoveredState
	hostScopeErrStreak int
	noSeedStreak       int

	// identityWaitCycles counts consecutive cycles without a resolved
	// network.device.id. When it reaches keysWaitCycles the probe gives up and
	// freezes identityGaveUp so metric series never change label set later.
	identityWaitCycles int
	identityGaveUp     bool
}

// NewSnmpPollProbe builds an snmp_poll probe from its raw params block.
func NewSnmpPollProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.snmp_poll")
	moduleLogger.Debug().
		Str("target", cfg.Target).
		Uint16("port", cfg.Port).
		Int("mibs", len(cfg.MIBs)).
		Int("custom_mappings", len(cfg.Custom)).
		Msg("Creating new SNMP poll probe")

	// Operator MIBs resolve custom-mapping names left empty in config
	// (#291: snmppoll adopts snmpmib like snmptrap — local files only,
	// never fetched over the network). Fail fast on an unresolvable OID:
	// a mapping with no name would emit an unidentifiable metric.
	if len(cfg.MibPaths) > 0 {
		resolver := snmpmib.Load(cfg.MibPaths, moduleLogger)
		for i := range cfg.Custom {
			if cfg.Custom[i].Metric != "" {
				continue
			}
			label, ok := resolver.Resolve(cfg.Custom[i].OID)
			if !ok {
				return nil, fmt.Errorf("snmp_poll custom_mappings[%d]: no 'metric' and OID %s not found in the configured MIBs", i, cfg.Custom[i].OID)
			}
			cfg.Custom[i].Metric = label
			moduleLogger.Debug().
				Str("oid", cfg.Custom[i].OID).
				Str("metric", label).
				Msg("custom mapping name resolved from operator MIBs")
		}
	} else {
		for i := range cfg.Custom {
			if cfg.Custom[i].Metric == "" {
				return nil, fmt.Errorf("snmp_poll custom_mappings[%d]: 'metric' is required without mib_paths", i)
			}
		}
	}

	entitySrc := newEntitySource(cfg, moduleLogger)
	probe := &snmppollProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     cfg.Target + ":" + strconv.Itoa(int(cfg.Port)),
		moduleLogger: moduleLogger,
		entitySource: entitySrc,
		newClient:    func(c *config) snmpClient { return newGosnmpClient(c) },
		hostScope:    defaultHostScope,
	}
	probe.SetProbeType(probeType)
	probe.SetEntitySource(entitySrc)
	return probe, nil
}

func (p *snmppollProbe) ShouldStart() bool {
	return true
}

func (p *snmppollProbe) GetInterval() time.Duration {
	return p.cfg.Interval
}

// OnStart does not connect: SNMP over UDP cannot detect device
// reachability at bind time, so reachability is reported per cycle via
// senhub.snmp.up instead.
func (p *snmppollProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("target", p.cfg.Target).
		Uint16("port", p.cfg.Port).
		Msg("SNMP poll probe started")

	if p.cfg.Discovery != nil {
		ctx, cancel := context.WithCancel(context.Background())
		p.discoveryCancel = cancel
		p.discoveryWG.Add(1)
		go p.discoveryLoop(ctx)
	}
	return nil
}

// Collect runs one poll cycle. A connection or poll failure is not a
// collection error: the probe emits senhub.snmp.up=0 so the outage is
// observable, mirroring the always-emit-up contract of the DB probes.
func (p *snmppollProbe) Collect() ([]data_store.DataPoint, error) {
	start := time.Now()

	client := p.newClient(p.cfg)
	up := float64(1)
	check := dbcommon.CheckOK
	var points []data_store.DataPoint

	keylessMode := p.identityGaveUp

	if err := client.Connect(); err != nil {
		up = 0
		check = dbcommon.ClassifyError(err)
		p.moduleLogger.Warn().Err(err).Str("target", p.instance).Msg("SNMP connect failed")
	} else {
		defer func() {
			if cErr := client.Close(); cErr != nil {
				p.moduleLogger.Warn().Err(cErr).Msg("error closing SNMP connection")
			}
		}()

		// Entity rail: refresh the topology snapshot on its own slow cadence,
		// reusing this already-connected client. Observe() (detector goroutine)
		// reads the cache; this only walks when topologyInterval has elapsed.
		p.entitySource.maybeSweep(client, start)
		identityResolved := p.entitySource.DeviceID() != ""

		var deviceID string
		var ifNames map[string]string
		var opts collectOptions

		if keylessMode {
			// Keyless mode: correlation tags are deliberately omitted forever,
			// even if a later sweep resolves the identity, so metric series never
			// change their label set.
			deviceID = ""
			ifNames = nil
			opts.SkipUnnamedInterfaces = false
		} else if identityResolved {
			deviceID = p.entitySource.DeviceID()
			ifNames = p.entitySource.InterfaceNames()
			// A device that names none of its interfaces never will: its
			// rows go out unnamed for good rather than not at all.
			opts.SkipUnnamedInterfaces = len(ifNames) > 0
		} else {
			// Waiting for the first successful sweep: no device or interface
			// metrics are emitted this cycle.
			deviceID = ""
			ifNames = nil
			opts.SkipUnnamedInterfaces = false
		}

		var answered bool
		if keylessMode || identityResolved {
			var metricPoints []data_store.DataPoint
			metricPoints, answered = collect(client, p.cfg, p.instance, deviceID, ifNames, start, p.moduleLogger, opts)
			points = append(points, metricPoints...)
		} else {
			// Poll enough to judge reachability, but discard decoded metrics
			// until the identity is resolved or the probe gives up.
			_, answered = collect(client, p.cfg, p.instance, "", nil, start, p.moduleLogger)
			// Only a device that answers without yielding an identity counts
			// toward giving up; an unreachable one is not an identity failure.
			if answered {
				p.noteIdentityWait()
			}
		}
		if !answered {
			// Connected (UDP always does) but zero responses: the device
			// is unreachable, filtered, or rejecting our credentials.
			up = 0
			// UDP gives no refusal: silence is all a filtered, unreachable
			// or wrong-community device ever returns.
			check = dbcommon.CheckTimeout
			p.moduleLogger.Warn().Str("target", p.instance).Msg("SNMP device answered no request this cycle")
		}
	}

	end := time.Now()
	points = append(points,
		data_store.DataPoint{Name: metricUp, Timestamp: end, Value: up, Tags: statusTags(p.instance)},
		data_store.DataPoint{Name: dbcommon.CheckResultMetric, Timestamp: end, Value: float64(check),
			Tags: p.checkResultTags()},
		data_store.DataPoint{Name: metricPollDuration, Timestamp: end, Value: end.Sub(start).Seconds(), Tags: statusTags(p.instance)},
	)

	if p.cfg.Discovery != nil {
		points = append(points, p.discoveryDataPoints(end)...)
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *snmppollProbe) OnShutdown(_ context.Context) error {
	if p.discoveryCancel != nil {
		p.discoveryCancel()
		p.discoveryWG.Wait()
	}
	return nil
}

func statusTags(instance string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "status"},
	}
}

// checkResultTags names the device the check is about once it has been
// resolved, so a reader seeing the device expire from the graph finds why in
// this series from the first failed poll on.
func (p *snmppollProbe) checkResultTags() []tags.Tag {
	t := statusTags(p.instance)
	if id := p.entitySource.DeviceID(); id != "" {
		t = append(t, tags.Tag{Key: "network.device.id", Value: id})
	}
	return t
}

// noteIdentityWait counts a cycle where the device answered but its identity
// is still unresolved, and gives up after keysWaitCycles.
func (p *snmppollProbe) noteIdentityWait() {
	p.identityWaitCycles++
	if p.identityWaitCycles < keysWaitCycles {
		return
	}
	p.identityGaveUp = true
	p.moduleLogger.Warn().
		Str("target", p.instance).
		Int("cycles", p.identityWaitCycles).
		Msg("giving up waiting for device identity; emitting device and interface metrics without correlation tags")
}

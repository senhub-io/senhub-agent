package snmppoll

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpmib"
	"senhub-agent.go/internal/agent/tags"
)

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

	// unregisterEntitySource detaches the entity source from the
	// process-global registry on shutdown (set in OnStart).
	unregisterEntitySource func()
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

	if cfg.Discovery != nil {
		// The crawl engine is merged but not yet wired to the poll
		// lifecycle (#156); without this warning an operator gets a
		// validated, silently inert block.
		moduleLogger.Warn().
			Str("target", cfg.Target).
			Msg("snmp_poll discovery is configured but not active yet (#156): the block is validated and ignored; per-device topology (LLDP/routes/bridge) still runs")
	}

	entitySrc := newEntitySource(cfg, moduleLogger)
	probe := &snmppollProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     cfg.Target + ":" + strconv.Itoa(int(cfg.Port)),
		moduleLogger: moduleLogger,
		entitySource: entitySrc,
		newClient:    func(c *config) snmpClient { return newGosnmpClient(c) },
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

// OnStart registers the probe's entity source so topology it discovers
// (Lot 5) folds into the agent's entity snapshot. There is no connect
// here: SNMP over UDP cannot detect device reachability at bind time, so
// reachability is reported per cycle via senhub.snmp.up instead.
func (p *snmppollProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().
		Str("target", p.cfg.Target).
		Uint16("port", p.cfg.Port).
		Msg("SNMP poll probe started")
	return nil
}

// Collect runs one poll cycle. A connection or poll failure is not a
// collection error: the probe emits senhub.snmp.up=0 so the outage is
// observable, mirroring the always-emit-up contract of the DB probes.
func (p *snmppollProbe) Collect() ([]data_store.DataPoint, error) {
	start := time.Now()

	client := p.newClient(p.cfg)
	up := float32(1)
	var points []data_store.DataPoint

	if err := client.Connect(); err != nil {
		up = 0
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
		// Done BEFORE collect so the metrics carry the freshly-resolved device
		// id / interface names (correlation tags); between sweeps the cached
		// values are reused.
		p.entitySource.maybeSweep(client, start)
		var answered bool
		points, answered = collect(client, p.cfg, p.instance,
			p.entitySource.DeviceID(), p.entitySource.InterfaceNames(), start, p.moduleLogger)
		if !answered {
			// Connected (UDP always does) but zero responses: the device
			// is unreachable, filtered, or rejecting our credentials.
			up = 0
			p.moduleLogger.Warn().Str("target", p.instance).Msg("SNMP device answered no request this cycle")
		}
	}

	end := time.Now()
	points = append(points,
		data_store.DataPoint{Name: metricUp, Timestamp: end, Value: up, Tags: statusTags(p.instance)},
		data_store.DataPoint{Name: metricPollDuration, Timestamp: end, Value: float32(end.Sub(start).Seconds()), Tags: statusTags(p.instance)},
	)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// OnShutdown unregisters the entity source so a stopped or reloaded probe
// stops heartbeating its cached topology (audit D4: dead devices never
// expired in the consumer; reloads duplicated sources).
func (p *snmppollProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
}

func statusTags(instance string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "status"},
	}
}

package snmppoll

import (
	"context"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
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

	probe := &snmppollProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     cfg.Target + ":" + strconv.Itoa(int(cfg.Port)),
		moduleLogger: moduleLogger,
		entitySource: newEntitySource(cfg),
		newClient:    func(c *config) snmpClient { return newGosnmpClient(c) },
	}
	probe.SetProbeType(probeType)
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
	entity.RegisterSource(p.entitySource)
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
		points = collect(client, p.cfg, p.instance, start, p.moduleLogger)
	}

	end := time.Now()
	points = append(points,
		data_store.DataPoint{Name: metricUp, Timestamp: end, Value: up, Tags: statusTags(p.instance)},
		data_store.DataPoint{Name: metricPollDuration, Timestamp: end, Value: float32(end.Sub(start).Seconds()), Tags: statusTags(p.instance)},
	)

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *snmppollProbe) OnShutdown(_ context.Context) error {
	return nil
}

func statusTags(instance string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: "status"},
	}
}

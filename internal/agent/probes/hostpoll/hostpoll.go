// Package hostpoll holds the body shared by the host polling probes —
// cpu, memory, network and logicaldisk. Those four probes differ in
// exactly one thing: the OS-specific Collector that reads the counters.
// Everything else (interval parsing, probe-name enrichment, shutdown,
// health, String) was duplicated verbatim in each package.
//
// The composition is deliberate: a probe package supplies a Spec and its
// Collector and gets the lifecycle back — it does not embed and override
// pieces of a parent, so there is exactly one implementation of the
// shared behaviour to read and to fix.
package hostpoll

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// defaultInterval is the polling period used when the config carries no
// usable `interval` value.
const defaultInterval = 30 * time.Second

// Collector is the OS-specific half of a host polling probe.
type Collector interface {
	Collect(timestamp time.Time) ([]datapoint.DataPoint, error)
	Close() error
}

// CollectorFactory builds the platform collector. Both loggers are handed
// over because the platform constructors disagree on which one they take
// (cpu wants the module logger's zerolog handle, the others the base
// logger) — the factory closure in each probe package picks.
type CollectorFactory func(config map[string]interface{}, baseLogger *logger.Logger, moduleLogger *logger.ModuleLogger) (Collector, error)

// Spec carries the per-probe identity of a host polling probe.
type Spec struct {
	// Module is the logger module name ("probe.cpu").
	Module string
	// Subject names the collected surface in error messages ("CPU").
	Subject string
	// TypeName is the prefix String() prints ("CPUProbe").
	TypeName string
	// NewCollector builds the platform collector.
	NewCollector CollectorFactory
}

// Probe is the shared implementation of a host polling probe.
type Probe struct {
	*types.BaseProbe
	collector Collector
	interval  time.Duration
	spec      Spec
}

// New builds a host polling probe from its Spec. The `interval` param is
// read in seconds through types.IntParam, so every numeric encoding a
// YAML or JSON decode can produce is honoured (#136).
func New(config map[string]interface{}, baseLogger *logger.Logger, spec Spec) (*Probe, error) {
	interval := defaultInterval
	if seconds, ok := types.IntParam(config, "interval"); ok {
		interval = time.Duration(seconds) * time.Second
	}

	moduleLogger := logger.NewModuleLogger(baseLogger, spec.Module)

	collector, err := spec.NewCollector(config, baseLogger, moduleLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s collector: %w", spec.Subject, err)
	}

	return &Probe{
		BaseProbe: &types.BaseProbe{},
		collector: collector,
		interval:  interval,
		spec:      spec,
	}, nil
}

// SetCollector replaces the platform collector. It exists so the probe
// packages can drive the shared lifecycle with a fake collector in their
// tests; production code sets the collector through the Spec factory.
func (p *Probe) SetCollector(c Collector) { p.collector = c }

func (p *Probe) ShouldStart() bool { return true }

func (p *Probe) GetInterval() time.Duration { return p.interval }

func (p *Probe) Collect() ([]datapoint.DataPoint, error) {
	metrics, err := p.collector.Collect(time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to collect %s metrics: %w", p.spec.Subject, err)
	}
	return p.EnrichDataPointsWithProbeName(metrics, p.GetName()), nil
}

func (p *Probe) OnStart(quitChannel chan struct{}) error { return nil }

func (p *Probe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *Probe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *Probe) String() string {
	return fmt.Sprintf("%s{name=%s, interval=%v}", p.spec.TypeName, p.GetName(), p.GetInterval())
}

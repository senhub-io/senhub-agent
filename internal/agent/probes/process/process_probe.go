// Package process implements the process monitor probe.
// It collects per-process and aggregated CPU/memory/fd metrics
// using gopsutil on Linux and Windows.
package process

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// processProbe collects OS process metrics on Linux and Windows.
type processProbe struct {
	*types.BaseProbe
	cfg    config
	logger *logger.ModuleLogger
}

// NewProcessProbe constructs a process probe from the YAML params block.
func NewProcessProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("process probe: invalid config: %w", err)
	}

	p := &processProbe{
		BaseProbe: &types.BaseProbe{},
		cfg:       cfg,
		logger:    logger.NewModuleLogger(baseLogger, "probe.process"),
	}
	p.SetProbeType("process")

	return p, nil
}

func (p *processProbe) ShouldStart() bool { return true }

func (p *processProbe) GetInterval() time.Duration { return p.cfg.interval }

func (p *processProbe) OnStart(_ chan struct{}) error { return nil }

func (p *processProbe) OnShutdown(_ context.Context) error { return nil }

func (p *processProbe) Collect() ([]data_store.DataPoint, error) {
	ts := time.Now()
	points, err := collect(ts, p.cfg, p.logger)
	if err != nil {
		return nil, fmt.Errorf("process probe: collect: %w", err)
	}
	return p.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

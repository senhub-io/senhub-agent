// Package process implements the process monitor probe.
// It collects per-process and aggregated CPU/memory/fd metrics
// using gopsutil on Linux and Windows.
package process

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// processProbe collects OS process metrics on Linux and Windows.
type processProbe struct {
	*types.BaseProbe
	cfg    config
	logger *logger.ModuleLogger

	// entitySrc is non-nil only in inventory mode (by_name / by_user). In
	// pure top_n or unfiltered mode the BaseProbe NoOp source is kept, so a
	// churning resource sample never floods Toise with process nodes.
	entitySrc *processEntitySource
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

	// Inventory intent = the operator named what to watch. Only then do the
	// monitored processes become Toise entities (the poller registers the
	// declared source; without it the NoOp fallback keeps the graph clean).
	if cfg.byName != nil || cfg.byUser != "" {
		p.entitySrc = newProcessEntitySource()
		p.SetEntitySource(p.entitySrc)
	}

	return p, nil
}

func (p *processProbe) ShouldStart() bool { return true }

func (p *processProbe) GetInterval() time.Duration { return p.cfg.interval }

func (p *processProbe) OnStart(_ chan struct{}) error { return nil }

func (p *processProbe) OnShutdown(_ context.Context) error { return nil }

func (p *processProbe) Collect() ([]data_store.DataPoint, error) {
	ts := time.Now()
	points, snaps, err := collect(ts, p.cfg, p.logger)
	if err != nil {
		return nil, fmt.Errorf("process probe: collect: %w", err)
	}

	if p.entitySrc != nil {
		hostID := ""
		if hi, herr := common.GetHostIdentity(); herr == nil {
			hostID = hi.ID
		}
		ents := make([]procEntity, 0, len(snaps))
		for _, s := range snaps {
			// A process with no readable creation time cannot carry the
			// contract identity {pid, creation.time}; skip it as an entity
			// rather than emit an ambiguous node (its metrics still ship).
			if s.createTime <= 0 {
				continue
			}
			ents = append(ents, procEntity{
				pid: s.pid, createTime: s.createTime, name: s.name, owner: s.owner,
			})
		}
		p.entitySrc.update(hostID, ents)
	}

	return p.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

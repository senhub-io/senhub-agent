//go:build windows || !windows

// internal/agent/probes/host/memoryProbe.go
//
package host

import (
	"context"
	"fmt"
	"runtime"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"time"
)

// memoryProbe représente le collecteur de métriques mémoire
type memoryProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
	collector osCollector
	interval  time.Duration
}

// NewMemoryProbe crée une nouvelle instance de Memory probe
func NewMemoryProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &memoryProbe{
		rawConfig: config,
		logger:    logger,
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newMemoryCollector(config, logger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newMemoryCollector(config, logger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Memory collector: %v", err)
	}

	return probe, nil
}

func (p *memoryProbe) GetName() string {
	return "memoryProbe"
}

func (p *memoryProbe) ShouldStart() bool {
	return true
}

func (p *memoryProbe) GetInterval() time.Duration {
	return p.interval
}

func (p *memoryProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()
	metrics, err := p.collector.Collect(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to collect Memory metrics: %v", err)
	}
	return metrics, nil
}

func (p *memoryProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *memoryProbe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *memoryProbe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *memoryProbe) String() string {
	return fmt.Sprintf("MemoryProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

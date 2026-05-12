//go:build windows || !windows

// internal/agent/probes/memory/memoryProbe.go
package memory

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
	*types.BaseProbe
	rawConfig map[string]interface{}
	logger    *logger.ModuleLogger
	collector osCollector
	interval  time.Duration
}

func (p *memoryProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// NewMemoryProbe crée une nouvelle instance de Memory probe
func NewMemoryProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &memoryProbe{
		BaseProbe: &types.BaseProbe{},
		rawConfig: config,
		logger:    logger.NewModuleLogger(baseLogger, "probe.memory"),
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newMemoryCollector(config, baseLogger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newMemoryCollector(config, baseLogger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Memory collector: %v", err)
	}

	return probe, nil
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "memory", "memory2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

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

	// Enrich datapoints with probe name and type tags
	enrichedMetrics := p.BaseProbe.EnrichDataPointsWithProbeName(metrics, p.GetName())

	return enrichedMetrics, nil
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

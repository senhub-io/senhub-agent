//go:build windows || !windows

// internal/agent/probes/host/cpuProbe.go
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

// cpuProbe représente le collecteur de métriques CPU
type cpuProbe struct {
	*types.BaseProbe // Ajout de BaseProbe
	rawConfig        map[string]interface{}
	moduleLogger     *logger.ModuleLogger
	collector        osCollector
	interval         time.Duration
}

// NewCpuProbe crée une nouvelle instance de CPU probe
func NewCpuProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}
	
	// Create module-specific logger for CPU probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.cpu")
	
	probe := &cpuProbe{
		BaseProbe:    &types.BaseProbe{}, // Initialisation de BaseProbe
		rawConfig:    config,
		moduleLogger: moduleLogger,
		interval:     interval,
	}
	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newCPUCollector(config, moduleLogger.Logger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newCPUCollector(config, moduleLogger.Logger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create CPU collector: %v", err)
	}
	return probe, nil
}

func (p *cpuProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}

func (p *cpuProbe) GetName() string {
	return "cpuProbe"
}

func (p *cpuProbe) ShouldStart() bool {
	return true
}

func (p *cpuProbe) GetInterval() time.Duration {
	return p.interval
}

func (p *cpuProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()
	metrics, err := p.collector.Collect(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to collect CPU metrics: %v", err)
	}

	// Enrich datapoints with probe name and send to strategies
	if p.OnDataPoints != nil {
		enrichedMetrics := p.EnrichDataPointsWithProbeName(metrics, "cpu")
		if err := p.OnDataPoints(enrichedMetrics, p); err != nil {
			return nil, fmt.Errorf("error handling data points: %v", err)
		}
	}

	return metrics, nil
}

func (p *cpuProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *cpuProbe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *cpuProbe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *cpuProbe) String() string {
	return fmt.Sprintf("CPUProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

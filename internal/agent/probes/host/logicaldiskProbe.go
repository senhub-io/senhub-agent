//go:build windows || !windows

package host

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// logicaldiskCollector defines the interface for OS-specific logicaldisk metric collectors
type logicaldiskCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}

// logicaldiskProbe represents the logical disk metrics collector
type logicaldiskProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
	collector logicaldiskCollector
	interval  time.Duration
}

func (p *logicaldiskProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}

// newLogicalDiskCollector creates a new Storage probe instance
func NewLogicalDiskProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &logicaldiskProbe{
		rawConfig: config,
		logger:    logger,
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newLogicalDiskCollector(config, logger)
	case "linux", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newLogicalDiskCollector(config, logger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create logicaldisk collector: %v", err)
	}
	return probe, nil
}

func (p *logicaldiskProbe) GetName() string {
	return "logicaldiskProbe"
}

func (p *logicaldiskProbe) ShouldStart() bool {
	return true
}

func (p *logicaldiskProbe) GetInterval() time.Duration {
	return p.interval
}

func (p *logicaldiskProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()
	metrics, err := p.collector.Collect(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to collect logicaldisk metrics: %v", err)
	}
	return metrics, nil
}

func (p *logicaldiskProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *logicaldiskProbe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *logicaldiskProbe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *logicaldiskProbe) String() string {
	return fmt.Sprintf("logicaldiskProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

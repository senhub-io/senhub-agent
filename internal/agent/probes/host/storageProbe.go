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

// storageCollector defines the interface for OS-specific storage metric collectors
type storageCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}

// storageProbe représente le collecteur de métriques de stockage
type storageProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
	collector storageCollector
	interval  time.Duration
}

// NewStorageProbe crée une nouvelle instance de Storage probe
func NewStorageProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &storageProbe{
		rawConfig: config,
		logger:    logger,
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newLogicalDiskCollector(config, logger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newLogicalDiskCollector(config, logger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create storage collector: %v", err)
	}
	return probe, nil
}

func (p *storageProbe) GetName() string {
	return "host_storage"
}

func (p *storageProbe) ShouldStart() bool {
	return true
}

func (p *storageProbe) GetInterval() time.Duration {
	return p.interval
}

func (p *storageProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()
	metrics, err := p.collector.Collect(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to collect storage metrics: %v", err)
	}
	return metrics, nil
}

func (p *storageProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *storageProbe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *storageProbe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *storageProbe) String() string {
	return fmt.Sprintf("StorageProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

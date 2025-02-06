//go:build windows || !windows

// internal/agent/probes/host/networkProbe.go
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

// networkProbe représente le collecteur de métriques réseau
type networkProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
	collector osNetworkCollector
	interval  time.Duration
}

// Interface pour les collecteurs spécifiques à l'OS
type osNetworkCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}

func (p *networkProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg"}
}

// NewNetworkProbe crée une nouvelle instance de Network probe
func NewNetworkProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &networkProbe{
		rawConfig: config,
		logger:    logger,
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newNetworkCollector(config, logger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newNetworkCollector(config, logger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create network collector: %v", err)
	}

	return probe, nil
}

func (p *networkProbe) GetName() string {
	return "networkProbe"
}

func (p *networkProbe) ShouldStart() bool {
	return true
}

func (p *networkProbe) GetInterval() time.Duration {
	return p.interval
}

func (p *networkProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()
	metrics, err := p.collector.Collect(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to collect network metrics: %v", err)
	}
	return metrics, nil
}

func (p *networkProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *networkProbe) OnShutdown(ctx context.Context) error {
	if p.collector != nil {
		return p.collector.Close()
	}
	return nil
}

func (p *networkProbe) IsHealthy() bool {
	_, err := p.Collect()
	return err == nil
}

func (p *networkProbe) String() string {
	return fmt.Sprintf("NetworkProbe{name=%s, interval=%v}", p.GetName(), p.GetInterval())
}

//go:build windows || !windows

package logicaldisk

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// normalizeFSType lowercases the filesystem name the OS reports so the
// system.filesystem.type attribute carries the same form on every
// platform: Windows reports "NTFS"/"ReFS" while Linux mount tables are
// already lower-case (ext4/xfs) — cross-platform rules filter on the
// lower-case token (#627).
func normalizeFSType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// logicaldiskCollector defines the interface for OS-specific logicaldisk metric collectors
type logicaldiskCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}

// logicaldiskProbe represents the logical disk metrics collector
type logicaldiskProbe struct {
	*types.BaseProbe
	rawConfig map[string]interface{}
	logger    *logger.ModuleLogger
	collector logicaldiskCollector
	interval  time.Duration
}

func (p *logicaldiskProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// newLogicalDiskCollector creates a new Storage probe instance
func NewLogicalDiskProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	interval := 30 * time.Second
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = time.Duration(cfgInterval) * time.Second
	}

	probe := &logicaldiskProbe{
		BaseProbe: &types.BaseProbe{},
		rawConfig: config,
		logger:    logger.NewModuleLogger(baseLogger, "probe.logicaldisk"),
		interval:  interval,
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		probe.collector, err = newLogicalDiskCollector(config, baseLogger)
	case "linux", "darwin", "freebsd", "openbsd", "netbsd":
		probe.collector, err = newLogicalDiskCollector(config, baseLogger)
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create logicaldisk collector: %v", err)
	}
	return probe, nil
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "logicaldisk", "logicaldisk2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

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

	// Enrich datapoints with probe name and type tags
	enrichedMetrics := p.BaseProbe.EnrichDataPointsWithProbeName(metrics, p.GetName())

	return enrichedMetrics, nil
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

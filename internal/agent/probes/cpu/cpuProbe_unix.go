//go:build !windows

// internal/agent/probes/host/cpuProbe_unix.go
package cpu

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type unixCollector struct {
	logger *logger.Logger
}

func newCPUCollector(config map[string]interface{}, logger *logger.Logger) (osCollector, error) {
	return &unixCollector{
		logger: logger,
	}, nil
}

func (u *unixCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	dataPoints := make([]data_store.DataPoint, 0, 20)

	baseTags, err := u.getBaseTags()
	if err != nil {
		return nil, err
	}

	// Try to collect CPU times, but don't fail if not available (e.g., on macOS)
	if err := u.collectCPUTimes(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect CPU times (may not be supported on this OS)")
	}

	// Collect CPU usage percentage (usually works on all platforms)
	if err := u.collectCPUUsage(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect CPU usage percentage")
	}

	// Collect load average (Unix-specific, usually works)
	if err := u.collectLoadAverage(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect load average")
	}

	// Collect per-core metrics
	if err := u.collectPerCoreMetrics(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect per-core metrics")
	}

	// Collect process count (cross-OS metric — needed for the host
	// dashboards' "running processes" panel à la node_exporter)
	if err := u.collectProcessesCount(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Warn().Err(err).Msg("Could not collect process count")
	}

	// If we couldn't collect any metrics at all, return an error
	if len(dataPoints) == 0 {
		return nil, fmt.Errorf("failed to collect any CPU metrics")
	}

	return dataPoints, nil
}

func (u *unixCollector) getBaseTags() ([]tags.Tag, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}
	return baseTags, nil
}

func (u *unixCollector) collectCPUTimes(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	times, err := cpu.Times(false)
	if err != nil {
		return fmt.Errorf("error getting CPU times: %v", err)
	}

	if len(times) == 0 {
		return nil
	}

	cpuTime := times[0]
	metrics := []struct {
		name  string
		value float64
	}{
		{"cpu_user", cpuTime.User},
		{"cpu_system", cpuTime.System},
		{"cpu_idle", cpuTime.Idle},
		{"cpu_nice", cpuTime.Nice},
		{"cpu_iowait", cpuTime.Iowait},
		{"cpu_irq", cpuTime.Irq},
		{"cpu_softirq", cpuTime.Softirq},
		{"cpu_steal", cpuTime.Steal},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     float32(metric.value),
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectCPUUsage(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return fmt.Errorf("error getting CPU percentage metrics: %v", err)
	}

	if len(cpuPercent) > 0 {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      "cpu_usage_total",
			Timestamp: timestamp,
			Value:     float32(cpuPercent[0]),
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectLoadAverage(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	loadAvg, err := load.Avg()
	if err != nil {
		return fmt.Errorf("error getting load average: %v", err)
	}

	metrics := []struct {
		name  string
		value float64
	}{
		{"cpu_load1", loadAvg.Load1},
		{"cpu_load5", loadAvg.Load5},
		{"cpu_load15", loadAvg.Load15},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     float32(metric.value),
			Tags:      baseTags,
		})
	}

	return nil
}

func (u *unixCollector) collectPerCoreMetrics(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	perCpuPercent, err := cpu.Percent(time.Second, true)
	if err != nil {
		return fmt.Errorf("error getting per-CPU metrics: %v", err)
	}

	for i, cpuPercent := range perCpuPercent {
		coreTags := append([]tags.Tag{}, baseTags...)
		coreTags = append(coreTags, tags.Tag{
			Key:   "core",
			Value: fmt.Sprintf("%d", i),
		})

		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      "cpu_core_usage",
			Timestamp: timestamp,
			Value:     float32(cpuPercent),
			Tags:      coreTags,
		})
	}

	return nil
}

// collectProcessesCount emits the count of running processes on the
// host. Sources `load.Misc()` which on Linux reads /proc/stat
// (procs_running + procs_blocked + ProcsTotal) — cheap, single file
// open. On Darwin and *BSD where /proc isn't available, gopsutil
// falls back to a sysctl call; same single-syscall cost.
//
// Mapped via cpu.yaml to OTel `system.processes.count` (gauge,
// unit `{process}`) per the OTel system semconv.
func (u *unixCollector) collectProcessesCount(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	misc, err := load.Misc()
	if err != nil {
		return fmt.Errorf("error getting process count: %v", err)
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "cpu_processes_total",
		Timestamp: timestamp,
		Value:     float32(misc.ProcsTotal),
		Tags:      baseTags,
	})
	return nil
}

func (u *unixCollector) Close() error {
	return nil
}

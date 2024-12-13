package probes

import (
	"context"
	"fmt"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"time"
)

type cpuProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
}

func NewCpuProbe(config map[string]interface{}, logger *logger.Logger) (Probe, error) {
	// No validation needed for this probe
	return &cpuProbe{
		rawConfig: config,
		logger:    logger,
	}, nil
}

func (m *cpuProbe) GetName() string {
	return "host_cpu"
}

func (m *cpuProbe) ShouldStart() bool {
	return true
}

func (m *cpuProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (m *cpuProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()

	// Retrieving common tags
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	// Collect total CPU percentage
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return nil, fmt.Errorf("error getting CPU metrics: %v", err)
	}

	// Collect CPU percentage per core
	perCpuPercent, err := cpu.Percent(time.Second, true)
	if err != nil {
		return nil, fmt.Errorf("error getting per-CPU metrics: %v", err)
	}

	// Collection of detailed CPU times
	cpuTimes, err := cpu.Times(false)
	if err != nil {
		return nil, fmt.Errorf("error getting CPU times: %v", err)
	}

	// System load collection
	loadAvg, err := load.Avg()
	if err != nil {
		return nil, fmt.Errorf("error getting load average: %v", err)
	}

	dataPoints := []data_store.DataPoint{
		{
			Name:      "cpu_usage_total",
			Timestamp: timestamp,
			Value:     float32(cpuPercent[0]),
			Tags:      baseTags,
		},
		{
			Name:      "cpu_load1",
			Timestamp: timestamp,
			Value:     float32(loadAvg.Load1),
			Tags:      baseTags,
		},
		{
			Name:      "cpu_load5",
			Timestamp: timestamp,
			Value:     float32(loadAvg.Load5),
			Tags:      baseTags,
		},
		{
			Name:      "cpu_load15",
			Timestamp: timestamp,
			Value:     float32(loadAvg.Load15),
			Tags:      baseTags,
		},
	}

	// Adding core metrics
	for i, cpuPercent := range perCpuPercent {
		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      fmt.Sprintf("cpu_usage_core_%d", i),
			Timestamp: timestamp,
			Value:     float32(cpuPercent),
			Tags:      baseTags,
		})
	}

	// Add detailed CPU times if available
	if len(cpuTimes) > 0 {
		times := cpuTimes[0]
		dataPoints = append(dataPoints,
			data_store.DataPoint{
				Name:      "cpu_user",
				Timestamp: timestamp,
				Value:     float32(times.User),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      "cpu_system",
				Timestamp: timestamp,
				Value:     float32(times.System),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      "cpu_idle",
				Timestamp: timestamp,
				Value:     float32(times.Idle),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      "cpu_iowait",
				Timestamp: timestamp,
				Value:     float32(times.Iowait),
				Tags:      baseTags,
			},
		)
	}

	return dataPoints, nil
}

func (m *cpuProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (m *cpuProbe) OnShutdown(ctx context.Context) error {
	return nil
}

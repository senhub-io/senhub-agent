package probes

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"

	"github.com/shirou/gopsutil/v3/mem"
)

type memoryProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
}

func NewMemoryProbe(config map[string]interface{}, logger *logger.Logger) (Probe, error) {
	// No validation needed for this probe
	return &memoryProbe{
		rawConfig: config,
		logger:    logger,
	}, nil
}

func (m *memoryProbe) GetName() string {
	return "host_memory"
}

func (m *memoryProbe) ShouldStart() bool {
	return true
}

func (m *memoryProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (m *memoryProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()

	// Retrieving common tags
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	// Virtual memory metrics retrieval
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("error getting virtual memory metrics: %v", err)
	}

	dataPoints := []data_store.DataPoint{
		{
			Name:      "mem_total",
			Timestamp: timestamp,
			Value:     float32(vmem.Total),
			Tags:      baseTags,
		},
		{
			Name:      "mem_available",
			Timestamp: timestamp,
			Value:     float32(vmem.Available),
			Tags:      baseTags,
		},
		{
			Name:      "mem_used",
			Timestamp: timestamp,
			Value:     float32(vmem.Used),
			Tags:      baseTags,
		},
		{
			Name:      "mem_used_percent",
			Timestamp: timestamp,
			Value:     float32(vmem.UsedPercent),
			Tags:      baseTags,
		},
		{
			Name:      "mem_free",
			Timestamp: timestamp,
			Value:     float32(vmem.Free),
			Tags:      baseTags,
		},
	}

	// Addition of Linux-specific metrics
	isLinux, err := common.IsLinux()
	if err == nil && isLinux {
		dataPoints = append(dataPoints, []data_store.DataPoint{
			{
				Name:      "mem_cached",
				Timestamp: timestamp,
				Value:     float32(vmem.Cached),
				Tags:      baseTags,
			},
			{
				Name:      "mem_buffers",
				Timestamp: timestamp,
				Value:     float32(vmem.Buffers),
				Tags:      baseTags,
			},
		}...)
	}

	// Retrieving swap/pagefile metrics
	swap, err := mem.SwapMemory()
	if err != nil {
		return nil, fmt.Errorf("error getting swap memory metrics: %v", err)
	}

	// Add swap/pagefile metrics
	dataPoints = append(dataPoints, []data_store.DataPoint{
		{
			Name:      "swap_total",
			Timestamp: timestamp,
			Value:     float32(swap.Total),
			Tags:      baseTags,
		},
		{
			Name:      "swap_used",
			Timestamp: timestamp,
			Value:     float32(swap.Used),
			Tags:      baseTags,
		},
		{
			Name:      "swap_free",
			Timestamp: timestamp,
			Value:     float32(swap.Free),
			Tags:      baseTags,
		},
		{
			Name:      "swap_used_percent",
			Timestamp: timestamp,
			Value:     float32(swap.UsedPercent),
			Tags:      baseTags,
		},
	}...)

	// Add Windows-specific metrics
	isWindows, err := common.IsWindows()
	if err == nil && isWindows {
		pageFile, err := mem.VirtualMemory() // On Windows, this includes pagefile information
		if err == nil {
			dataPoints = append(dataPoints, []data_store.DataPoint{
				{
					Name:      "pagefile_total",
					Timestamp: timestamp,
					Value:     float32(pageFile.Total),
					Tags:      baseTags,
				},
				{
					Name:      "pagefile_available",
					Timestamp: timestamp,
					Value:     float32(pageFile.Available),
					Tags:      baseTags,
				},
			}...)
		}
	}

	return dataPoints, nil
}

func (m *memoryProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *memoryProbe) OnShutdown(ctx context.Context) error {
	return nil
}

//go:build !windows

// internal/agent/probes/host/memoryProbe_unix.go
package memory

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type unixMemoryCollector struct {
	logger *logger.Logger
}

func newMemoryCollector(config map[string]interface{}, logger *logger.Logger) (osCollector, error) {
	return &unixMemoryCollector{
		logger: logger,
	}, nil
}

func (u *unixMemoryCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	dataPoints := make([]data_store.DataPoint, 0, 15)

	baseTags, err := u.getBaseTags()
	if err != nil {
		return nil, err
	}

	if err := u.collectVirtualMemory(&dataPoints, timestamp, baseTags); err != nil {
		return nil, err
	}

	if err := u.collectSwapMemory(&dataPoints, timestamp, baseTags); err != nil {
		return nil, err
	}

	return dataPoints, nil
}

func (u *unixMemoryCollector) getBaseTags() ([]tags.Tag, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}
	return baseTags, nil
}

func (u *unixMemoryCollector) collectVirtualMemory(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return fmt.Errorf("error getting virtual memory metrics: %v", err)
	}

	// NOTE: vmem.Available is intentionally NOT emitted on Unix. The
	// memory.yaml definition maps both memory_available and memory_free to
	// system.memory.state=free, so emitting both produces duplicate
	// Prometheus time series with conflicting values. memory_available
	// remains available on the Windows path where "available" is the
	// canonical Windows term and harmonization to state=free is meaningful.
	metrics := []struct {
		name  string
		value uint64
	}{
		{"memory_total", vmem.Total},
		{"memory_used", vmem.Used},
		{"memory_free", vmem.Free},
		{"memory_cached", vmem.Cached},
		{"memory_buffers", vmem.Buffers},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     float32(metric.value),
			Tags:      baseTags,
		})
	}

	// Ajouter le pourcentage d'utilisation
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "memory_used_percent",
		Timestamp: timestamp,
		Value:     float32(vmem.UsedPercent),
		Tags:      baseTags,
	})

	return nil
}

func (u *unixMemoryCollector) collectSwapMemory(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	swap, err := mem.SwapMemory()
	if err != nil {
		return fmt.Errorf("error getting swap memory metrics: %v", err)
	}

	metrics := []struct {
		name  string
		value uint64
	}{
		{"swap_total", swap.Total},
		{"swap_used", swap.Used},
		{"swap_free", swap.Free},
	}

	for _, metric := range metrics {
		*dataPoints = append(*dataPoints, data_store.DataPoint{
			Name:      metric.name,
			Timestamp: timestamp,
			Value:     float32(metric.value),
			Tags:      baseTags,
		})
	}

	// Ajouter le pourcentage d'utilisation du swap
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "swap_used_percent",
		Timestamp: timestamp,
		Value:     float32(swap.UsedPercent),
		Tags:      baseTags,
	})

	return nil
}

func (u *unixMemoryCollector) Close() error {
	return nil
}

//go:build !windows

// internal/agent/probes/host/memoryProbe_unix.go
package memory

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type unixMemoryCollector struct {
	logger *logger.Logger
	// lastFaults holds the previous cumulative page-fault count. The
	// definition declares the metric in 1/s, the way the Windows
	// collector already reports it, so the rate is derived here.
	lastFaults   float64
	lastFaultsAt time.Time
	haveFaults   bool
}

func newMemoryCollector(config map[string]interface{}, logger *logger.Logger) (hostpoll.Collector, error) {
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

	// Page faults, which a native Zabbix agent has no key for but every
	// memory-pressure reading wants. Absent on some kernels, so a
	// failure here does not fail the collection.
	if err := u.collectPageFaults(&dataPoints, timestamp, baseTags); err != nil {
		u.logger.Debug().Err(err).Msg("Page faults not available on this OS")
	}

	return dataPoints, nil
}

// collectPageFaults turns the kernel's cumulative fault count into the
// per-second rate the definition declares. The first reading has
// nothing to subtract from and emits nothing.
func (u *unixMemoryCollector) collectPageFaults(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	swap, err := mem.SwapMemory()
	if err != nil {
		return fmt.Errorf("error getting page fault counters: %w", err)
	}
	curr := float64(swap.PgFault)
	prev, prevAt, had := u.lastFaults, u.lastFaultsAt, u.haveFaults
	u.lastFaults, u.lastFaultsAt, u.haveFaults = curr, timestamp, true
	if !had {
		return nil
	}
	elapsed := timestamp.Sub(prevAt).Seconds()
	if elapsed <= 0 || curr < prev {
		// No elapsed time, or a counter reset by a reboot; skip the
		// round rather than emit a negative rate.
		return nil
	}
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "memory_page_faults",
		Timestamp: timestamp,
		Value:     (curr - prev) / elapsed,
		Tags:      baseTags,
	})
	return nil
}

func (u *unixMemoryCollector) getBaseTags() ([]tags.Tag, error) {
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}
	return baseTags, nil
}

func (u *unixMemoryCollector) collectVirtualMemory(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return fmt.Errorf("error getting virtual memory metrics: %w", err)
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
			Value:     float64(metric.value),
			Tags:      baseTags,
		})
	}

	// Ajouter le pourcentage d'utilisation
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "memory_used_percent",
		Timestamp: timestamp,
		Value:     vmem.UsedPercent,
		Tags:      baseTags,
	})

	return nil
}

func (u *unixMemoryCollector) collectSwapMemory(dataPoints *[]data_store.DataPoint, timestamp time.Time, baseTags []tags.Tag) error {
	swap, err := mem.SwapMemory()
	if err != nil {
		return fmt.Errorf("error getting swap memory metrics: %w", err)
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
			Value:     float64(metric.value),
			Tags:      baseTags,
		})
	}

	// Ajouter le pourcentage d'utilisation du swap
	*dataPoints = append(*dataPoints, data_store.DataPoint{
		Name:      "swap_used_percent",
		Timestamp: timestamp,
		Value:     swap.UsedPercent,
		Tags:      baseTags,
	})

	return nil
}

func (u *unixMemoryCollector) Close() error {
	return nil
}

package probes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"

	"github.com/shirou/gopsutil/v3/disk"
)

type storageProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
}

func NewStorageProbe(config map[string]interface{}, logger *logger.Logger) (Probe, error) {
	// No validation needed for this probe
	return &storageProbe{
		rawConfig: config,
		logger:    logger,
	}, nil
}

func (m *storageProbe) GetName() string {
	return "host_storage"
}

func (m *storageProbe) ShouldStart() bool {
	return true
}

func (m *storageProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (m *storageProbe) Collect() ([]data_store.DataPoint, error) {
	timestamp := time.Now()

	// Get common host tags
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	// Check if we're on Windows
	isWindows, err := common.IsWindows()
	if err != nil {
		return nil, fmt.Errorf("error determining OS: %v", err)
	}

	// Get partitions
	partitions, err := disk.Partitions(false) // false = physical partitions only
	if err != nil {
		return nil, fmt.Errorf("error getting partitions: %v", err)
	}

	var dataPoints []data_store.DataPoint

	// Collect metrics for each partition
	for _, partition := range partitions {
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			continue // Skip this partition if we can't get usage info
		}

		// Clean up the mountpoint/device name for metric naming
		metricPrefix := partition.Mountpoint
		if isWindows {
			// For Windows, remove ":" and "\" from drive letters
			metricPrefix = strings.ReplaceAll(metricPrefix, ":", "")
			metricPrefix = strings.ReplaceAll(metricPrefix, "\\", "")
		} else {
			// For Unix systems, replace "/" with "_"
			metricPrefix = strings.Trim(metricPrefix, "/")
			if metricPrefix == "" {
				metricPrefix = "root"
			}
			metricPrefix = strings.ReplaceAll(metricPrefix, "/", "_")
		}

		// Add space usage metrics
		dataPoints = append(dataPoints,
			data_store.DataPoint{
				Name:      fmt.Sprintf("%s_total_bytes", metricPrefix),
				Timestamp: timestamp,
				Value:     float32(usage.Total),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      fmt.Sprintf("%s_used_bytes", metricPrefix),
				Timestamp: timestamp,
				Value:     float32(usage.Used),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      fmt.Sprintf("%s_free_bytes", metricPrefix),
				Timestamp: timestamp,
				Value:     float32(usage.Free),
				Tags:      baseTags,
			},
			data_store.DataPoint{
				Name:      fmt.Sprintf("%s_used_percent", metricPrefix),
				Timestamp: timestamp,
				Value:     float32(usage.UsedPercent),
				Tags:      baseTags,
			},
		)

		// Add inode metrics only for Unix systems
		if !isWindows && usage.InodesTotal > 0 {
			dataPoints = append(dataPoints,
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_inodes_total", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(usage.InodesTotal),
					Tags:      baseTags,
				},
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_inodes_used", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(usage.InodesUsed),
					Tags:      baseTags,
				},
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_inodes_free", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(usage.InodesFree),
					Tags:      baseTags,
				},
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_inodes_used_percent", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(usage.InodesUsedPercent),
					Tags:      baseTags,
				},
			)
		}
	}

	// Get IO statistics
	ioStats, err := disk.IOCounters()
	if err == nil { // Don't return error if we can't get IO stats
		for deviceName, stat := range ioStats {
			// Clean up device name for metric naming
			metricPrefix := deviceName
			if isWindows {
				// Remove potentially problematic characters from Windows device names
				metricPrefix = strings.ReplaceAll(metricPrefix, ":", "")
				metricPrefix = strings.ReplaceAll(metricPrefix, "\\", "")
				metricPrefix = strings.ReplaceAll(metricPrefix, " ", "_")
			}

			dataPoints = append(dataPoints,
				// Read metrics
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_reads_completed", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(stat.ReadCount),
					Tags:      baseTags,
				},
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_bytes_read", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(stat.ReadBytes),
					Tags:      baseTags,
				},
				// Write metrics
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_writes_completed", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(stat.WriteCount),
					Tags:      baseTags,
				},
				data_store.DataPoint{
					Name:      fmt.Sprintf("%s_bytes_written", metricPrefix),
					Timestamp: timestamp,
					Value:     float32(stat.WriteBytes),
					Tags:      baseTags,
				},
			)

			// Add time-based metrics only if they are available (might not be on all systems)
			if stat.ReadTime > 0 || stat.WriteTime > 0 {
				dataPoints = append(dataPoints,
					data_store.DataPoint{
						Name:      fmt.Sprintf("%s_read_time", metricPrefix),
						Timestamp: timestamp,
						Value:     float32(stat.ReadTime),
						Tags:      baseTags,
					},
					data_store.DataPoint{
						Name:      fmt.Sprintf("%s_write_time", metricPrefix),
						Timestamp: timestamp,
						Value:     float32(stat.WriteTime),
						Tags:      baseTags,
					},
				)
			}

			// Add IO time metrics only if they are available
			if stat.IoTime > 0 {
				dataPoints = append(dataPoints,
					data_store.DataPoint{
						Name:      fmt.Sprintf("%s_io_time", metricPrefix),
						Timestamp: timestamp,
						Value:     float32(stat.IoTime),
						Tags:      baseTags,
					},
				)
			}

			if stat.WeightedIO > 0 {
				dataPoints = append(dataPoints,
					data_store.DataPoint{
						Name:      fmt.Sprintf("%s_weighted_io", metricPrefix),
						Timestamp: timestamp,
						Value:     float32(stat.WeightedIO),
						Tags:      baseTags,
					},
				)
			}
		}
	}

	return dataPoints, nil
}

func (m *storageProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *storageProbe) OnShutdown(ctx context.Context) error {
	return nil
}

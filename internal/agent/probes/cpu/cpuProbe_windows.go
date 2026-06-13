//go:build windows

package cpu

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// Instance filters configuration
var instanceFilters = struct {
	Include []string
	Exclude []string
}{
	Include: []string{}, // List of instances to include (empty = all)
	Exclude: []string{}, // List of instances to exclude
}

// MetricDefinition defines a performance counter with its path and instance
type MetricDefinition struct {
	path     string
	instance string
}

// Performance counters definition
var counterPaths = map[string]MetricDefinition{
	"processor_time": {
		path:     "\\Processor\\% Processor Time",
		instance: "*",
	},
	"user_time": {
		path:     "\\Processor\\% User Time",
		instance: "*",
	},
	"privileged_time": {
		path:     "\\Processor\\% Privileged Time",
		instance: "*",
	},
	"interrupt_time": {
		path:     "\\Processor\\% Interrupt Time",
		instance: "*",
	},
	"dpc_time": {
		path:     "\\Processor\\% DPC Time",
		instance: "*",
	},
	"dpc_rate": {
		path:     "\\Processor\\DPC Rate",
		instance: "*",
	},
	"dpc_queued": {
		path:     "\\Processor\\DPCs Queued/sec",
		instance: "*",
	},
	"interrupt_sec": {
		path:     "\\Processor\\Interrupts/sec",
		instance: "*",
	},
	"processor_queue_length": {
		path:     "\\System\\Processor Queue Length",
		instance: "",
	},
}

// CPUMetrics contains all collected CPU metrics
type CPUMetrics struct {
	metrics map[string]float64
}

// NewCPUMetrics creates a new instance of CPUMetrics
func NewCPUMetrics() *CPUMetrics {
	return &CPUMetrics{
		metrics: make(map[string]float64),
	}
}

// SetMetric sets the value of a metric
func (c *CPUMetrics) SetMetric(name string, value float64) {
	c.metrics[name] = value
}

// GetMetric retrieves the value of a metric
func (c *CPUMetrics) GetMetric(name string) float64 {
	return c.metrics[name]
}

type pathInfo struct {
	path     string
	instance string
}

type windowsCollector struct {
	query       *pdh.Query
	paths       map[string]pathInfo
	mu          sync.Mutex
	initialized bool
	logger      *logger.ModuleLogger
}

func newCPUCollector(config map[string]interface{}, baseLogger *logger.Logger) (osCollector, error) {
	// Initialize PDH logger
	pdh.InitializePDHLogger(baseLogger)

	// Create module logger for host probes
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.host")

	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	collector := &windowsCollector{
		query:  query,
		paths:  make(map[string]pathInfo),
		logger: moduleLogger,
	}

	if err := collector.initializeCounters(); err != nil {
		query.Close()
		return nil, err
	}

	if err := query.Collect(); err != nil {
		query.Close()
		return nil, fmt.Errorf("failed initial collection: %v", err)
	}

	return collector, nil
}

// shouldIncludeInstance checks if an instance should be included according to filters
// Improved shouldIncludeInstance
func (w *windowsCollector) shouldIncludeInstance(instance string) bool {
	w.logger.Debug().
		Str("instance", instance).
		Strs("include_filters", instanceFilters.Include).
		Strs("exclude_filters", instanceFilters.Exclude).
		Msg("Checking instance against filters")

	// If the inclusion list is empty, everything is included by default
	if len(instanceFilters.Include) == 0 {
		// Only check exclusions
		for _, excludedInstance := range instanceFilters.Exclude {
			if excludedInstance == instance {
				w.logger.Debug().Str("instance", instance).Msg("Instance excluded by filter")
				return false
			}
		}
		w.logger.Debug().Str("instance", instance).Msg("Instance included (no include filters, not in exclude list)")
		return true
	}

	// If the inclusion list is not empty, check if the instance is in it
	for _, includedInstance := range instanceFilters.Include {
		if includedInstance == instance {
			// Check that the instance is not in the exclusion list
			for _, excludedInstance := range instanceFilters.Exclude {
				if excludedInstance == instance {
					w.logger.Debug().Str("instance", instance).Msg("Instance found in include list but excluded")
					return false
				}
			}
			w.logger.Debug().Str("instance", instance).Msg("Instance found in include list and not excluded")
			return true
		}
	}

	w.logger.Debug().Str("instance", instance).Msg("Instance not in include list")
	return false
}

func (w *windowsCollector) initializeCounters() error {
	w.logger.Debug().Msg("Initializing CPU probe with counters")

	for metricName, def := range counterPaths {
		if def.instance == "*" {
			parts := strings.Split(def.path, "\\")
			if len(parts) < 3 {
				return fmt.Errorf("invalid counter path format: %s", def.path)
			}
			objectName := parts[1]

			instances, err := pdh.GetInstancesList(objectName, false)
			if err != nil {
				return fmt.Errorf("failed to get %s instances: %v", objectName, err)
			}

			hasTotal := false
			for _, instance := range instances {
				if instance == "_Total" {
					hasTotal = true
					break
				}
			}
			if !hasTotal {
				instances = append(instances, "_Total")
			}

			for _, instance := range instances {
				if !w.shouldIncludeInstance(instance) {
					w.logger.Debug().Str("instance", instance).Msg("Instance skipped due to filters")
					continue
				}

				path := pdh.BuildCounterPath(def.path, instance)
				// Create a unique key for each metric/instance combination
				uniqueKey := fmt.Sprintf("%s|%s", metricName, instance)
				w.paths[uniqueKey] = pathInfo{
					path:     path,
					instance: instance,
				}

				w.logger.Debug().
					Str("metric", metricName).
					Str("path", path).
					Str("instance", instance).
					Msg("Adding counter")
				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (instance %s): %v",
						metricName, instance, err)
				}
			}
		} else {
			path := pdh.BuildCounterPath(def.path, def.instance)
			w.paths[metricName] = pathInfo{
				path:     path,
				instance: def.instance,
			}

			w.logger.Debug().
				Str("metric", metricName).
				Str("path", path).
				Msg("Adding counter")
			if err := w.query.AddCounter(path); err != nil {
				return fmt.Errorf("failed to add counter %s: %v", metricName, err)
			}
		}
	}
	return nil
}

func (w *windowsCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logger.Debug().
		Time("timestamp", timestamp).
		Int("paths_count", len(w.paths)).
		Msg("Starting metrics collection")

	// Log registered paths
	if w.logger.Debug().Enabled() {
		for name, pathInfo := range w.paths {
			w.logger.Debug().
				Str("metric", name).
				Str("path", pathInfo.path).
				Str("instance", pathInfo.instance).
				Msg("Registered path")
		}
	}

	if !w.initialized {
		w.logger.Debug().Msg("Initializing first collection")
		if err := w.query.Collect(); err != nil {
			return nil, fmt.Errorf("failed initial sample collection: %v", err)
		}
		time.Sleep(1 * time.Second)
		w.initialized = true
		w.logger.Debug().Msg("First collection initialized")
	}

	w.logger.Debug().Msg("Collecting metrics")
	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %v", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}
	w.logger.Debug().Interface("base_tags", baseTags).Msg("Got base tags")

	metrics := NewCPUMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	w.logger.Debug().Msg("Processing individual metrics")
	for name, pathInfo := range w.paths {
		w.logger.Debug().
			Str("metric", name).
			Str("path", pathInfo.path).
			Msg("Processing metric")

		metricName := strings.Split(name, "|")[0]

		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			w.logger.Debug().
				Str("metric", name).
				Err(err).
				Msg("Error getting counter value")
			continue
		}

		// Normalize metric names and values
		normalizedName, normalizedValue := w.normalizeMetric(metricName, pathInfo.instance, value)
		if normalizedName == "" {
			// Skip this metric if it shouldn't be reported
			continue
		}

		// Preparing tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Adding core tag for ALL CPU metrics for consistent filtering
		// System metrics: only keep _Total instance to avoid duplicates
		isSystemMetric := normalizedName == "cpu_dpc_rate" || normalizedName == "cpu_dpc_queued" ||
			normalizedName == "cpu_interrupts" || normalizedName == "cpu_queue_length"

		// Skip per-core instances of system metrics (keep only _Total)
		if isSystemMetric && pathInfo.instance != "" && pathInfo.instance != "_Total" {
			w.logger.Debug().
				Str("metric", normalizedName).
				Str("instance", pathInfo.instance).
				Msg("Skipping per-core instance of system metric")
			continue // Skip this metric completely
		}

		if pathInfo.instance != "" && pathInfo.instance != "_Total" && !isSystemMetric {
			// True per-core metrics: use the core number
			w.logger.Debug().Str("instance", pathInfo.instance).Msg("Adding per-core tag")
			metricTags = append(metricTags, tags.Tag{
				Key:     "core",
				Value:   pathInfo.instance,
				Private: false,
			})
		} else {
			// Global/system metrics: use "total" tag for consistent filtering
			w.logger.Debug().Str("normalized_name", normalizedName).Msg("Adding total core tag")
			metricTags = append(metricTags, tags.Tag{
				Key:     "core",
				Value:   "total",
				Private: false,
			})
		}

		// Store the metric
		metrics.SetMetric(name, normalizedValue)

		dataPoint := data_store.DataPoint{
			Name:      normalizedName,
			Timestamp: timestamp,
			Value:     normalizedValue,
			Tags:      metricTags,
		}
		dataPoints = append(dataPoints, dataPoint)

		w.logger.Debug().
			Str("metric", name).
			Float64("value", value).
			Interface("tags", metricTags).
			Msg("Collected metric")
	}

	// Process count — sourced via gopsutil/process.Pids which on
	// Windows wraps EnumProcesses. Cheap single syscall (~10s of
	// microseconds for a few hundred processes). Mapped via cpu.yaml
	// to OTel system.processes.count.
	if pids, perr := process.Pids(); perr == nil {
		coreTotalTags := append([]tags.Tag{}, baseTags...)
		coreTotalTags = append(coreTotalTags, tags.Tag{Key: "core", Value: "total"})
		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      "cpu_processes_total",
			Timestamp: timestamp,
			Value:     float64(len(pids)),
			Tags:      coreTotalTags,
		})
	} else {
		w.logger.Warn().Err(perr).Msg("Could not collect process count")
	}

	w.logger.Debug().
		Int("total_metrics", len(dataPoints)).
		Msg("Collection completed")
	return dataPoints, nil
}

// normalizeMetric converts Windows PDH metrics to standardized names and validates values
func (w *windowsCollector) normalizeMetric(metricName, instance string, value float64) (string, float64) {
	// Validate percentage values (should be between 0-100)
	normalizedValue := value
	if strings.Contains(metricName, "time") || strings.Contains(metricName, "percent") {
		// Clamp percentage values to valid range
		if normalizedValue < 0 {
			normalizedValue = 0
		} else if normalizedValue > 100 {
			normalizedValue = 100
		}
	}

	// Map Windows PDH metrics to Unix-compatible names
	switch metricName {
	case "processor_time":
		if instance == "_Total" {
			return "cpu_usage_total", normalizedValue
		} else {
			return "cpu_core_usage", normalizedValue
		}
	case "user_time":
		if instance == "_Total" {
			return "cpu_user", normalizedValue
		} else {
			return "cpu_core_user", normalizedValue
		}
	case "privileged_time":
		if instance == "_Total" {
			return "cpu_system", normalizedValue
		} else {
			return "cpu_core_system", normalizedValue
		}
	case "interrupt_time":
		if instance == "_Total" {
			return "cpu_irq", normalizedValue
		} else {
			return "cpu_core_irq", normalizedValue
		}
	case "dpc_time":
		if instance == "_Total" {
			return "cpu_softirq", normalizedValue
		} else {
			return "cpu_core_softirq", normalizedValue
		}
	case "dpc_rate":
		return "cpu_dpc_rate", normalizedValue
	case "dpc_queued":
		return "cpu_dpc_queued", normalizedValue
	case "interrupt_sec":
		return "cpu_interrupts", normalizedValue
	case "processor_queue_length":
		return "cpu_queue_length", normalizedValue
	default:
		// Return empty string to skip unknown metrics
		return "", 0
	}
}

func (w *windowsCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

//go:build windows

package cpu

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
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

// Performance counters definition
var counterPaths = map[string]pdh.CounterDef{
	"processor_time": {
		Path:     "\\Processor\\% Processor Time",
		Instance: "*",
	},
	"user_time": {
		Path:     "\\Processor\\% User Time",
		Instance: "*",
	},
	"privileged_time": {
		Path:     "\\Processor\\% Privileged Time",
		Instance: "*",
	},
	"interrupt_time": {
		Path:     "\\Processor\\% Interrupt Time",
		Instance: "*",
	},
	"dpc_time": {
		Path:     "\\Processor\\% DPC Time",
		Instance: "*",
	},
	"dpc_rate": {
		Path:     "\\Processor\\DPC Rate",
		Instance: "*",
	},
	"dpc_queued": {
		Path:     "\\Processor\\DPCs Queued/sec",
		Instance: "*",
	},
	"interrupt_sec": {
		Path:     "\\Processor\\Interrupts/sec",
		Instance: "*",
	},
	"processor_queue_length": {
		Path:     "\\System\\Processor Queue Length",
		Instance: "",
	},
	// Context switches and idle time were reported on Linux and not
	// here, so a Windows host carried two items that could never
	// receive a value while the native agent showed both.
	"context_switches": {
		Path:     "\\System\\Context Switches/sec",
		Instance: "",
	},
	"idle_time": {
		Path:     "\\Processor\\% Idle Time",
		Instance: "*",
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
	query          *pdh.Query
	paths          map[string]pdh.CounterDef
	mu             sync.Mutex
	initialized    bool
	logger         *logger.ModuleLogger
	perCoreDetails bool
}

func newCPUCollector(config map[string]interface{}, baseLogger *logger.Logger) (hostpoll.Collector, error) {
	// Initialize PDH logger
	pdh.InitializePDHLogger(baseLogger)

	// Create module logger for host probes
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.host")

	perCoreDetails, _ := types.BoolParam(config, "per_core_details")

	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %w", err)
	}

	collector := &windowsCollector{
		query:          query,
		paths:          make(map[string]pdh.CounterDef),
		logger:         moduleLogger,
		perCoreDetails: perCoreDetails,
	}

	if err := collector.initializeCounters(); err != nil {
		query.Close()
		return nil, err
	}

	// PDH rate counters have no sample until PdhCollectQueryData has run
	// twice, so PDH_NO_DATA here is expected on a freshly-booted host or a
	// cold CI runner — failing construction on it broke probe startup
	// (#590). Collect primes the query again before reading values.
	if err := query.Collect(); err != nil {
		if !errors.Is(err, pdh.ErrNoData) {
			query.Close()
			return nil, fmt.Errorf("failed initial collection: %w", err)
		}
		moduleLogger.Warn().Err(err).Msg("No PDH sample available at construction, deferring priming to first collection")
	}

	return collector, nil
}

// shouldIncludeInstance checks if an instance should be included according to filters
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
		if def.Instance == "*" {
			parts := strings.Split(def.Path, "\\")
			if len(parts) < 3 {
				return fmt.Errorf("invalid counter path format: %s", def.Path)
			}
			objectName := parts[1]

			instances, err := pdh.GetInstancesList(objectName, false)
			if err != nil {
				return fmt.Errorf("failed to get %s instances: %w", objectName, err)
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

				if !shouldCollectCPUCounter(metricName, instance, w.perCoreDetails) {
					w.logger.Debug().
						Str("metric", metricName).
						Str("instance", instance).
						Msg("Per-core breakdown metric skipped")
					continue
				}

				path := pdh.BuildCounterPath(def.Path, instance)
				// Create a unique key for each metric/instance combination
				uniqueKey := fmt.Sprintf("%s|%s", metricName, instance)
				w.paths[uniqueKey] = pdh.CounterDef{
					Path:     path,
					Instance: instance,
				}

				w.logger.Debug().
					Str("metric", metricName).
					Str("path", path).
					Str("instance", instance).
					Msg("Adding counter")
				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (instance %s): %w",
						metricName, instance, err)
				}
			}
		} else {
			path := pdh.BuildCounterPath(def.Path, def.Instance)
			w.paths[metricName] = pdh.CounterDef{
				Path:     path,
				Instance: def.Instance,
			}

			w.logger.Debug().
				Str("metric", metricName).
				Str("path", path).
				Msg("Adding counter")
			if err := w.query.AddCounter(path); err != nil {
				return fmt.Errorf("failed to add counter %s: %w", metricName, err)
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

	if w.logger.Debug().Enabled() {
		for name, pathInfo := range w.paths {
			w.logger.Debug().
				Str("metric", name).
				Str("path", pathInfo.Path).
				Str("instance", pathInfo.Instance).
				Msg("Registered path")
		}
	}

	if !w.initialized {
		w.logger.Debug().Msg("Initializing first collection")
		if err := pdh.PrimeSamples(w.query); err != nil {
			return nil, err
		}
		w.initialized = true
		w.logger.Debug().Msg("First collection initialized")
	}

	w.logger.Debug().Msg("Collecting metrics")
	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %w", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}
	w.logger.Debug().Interface("base_tags", baseTags).Msg("Got base tags")

	metrics := NewCPUMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	w.logger.Debug().Msg("Processing individual metrics")
	for name, pathInfo := range w.paths {
		w.logger.Debug().
			Str("metric", name).
			Str("path", pathInfo.Path).
			Msg("Processing metric")

		metricName := strings.Split(name, "|")[0]

		value, err := w.query.GetCounterValue(pathInfo.Path)
		if err != nil {
			w.logger.Debug().
				Str("metric", name).
				Err(err).
				Msg("Error getting counter value")
			continue
		}

		// Normalize metric names and values
		normalizedName, normalizedValue := w.normalizeMetric(metricName, pathInfo.Instance, value)
		if normalizedName == "" {
			// Skip this metric if it shouldn't be reported
			continue
		}

		// Preparing tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Adding core tag for ALL CPU metrics for consistent filtering
		isSystemMetric := normalizedName == "cpu_dpc_rate" || normalizedName == "cpu_dpc_queued" ||
			normalizedName == "cpu_interrupts" || normalizedName == "cpu_queue_length" ||
			normalizedName == "cpu_context_switches"

		// Skip per-core instances of system metrics (keep only _Total)
		if isSystemMetric && pathInfo.Instance != "" && pathInfo.Instance != "_Total" {
			w.logger.Debug().
				Str("metric", normalizedName).
				Str("instance", pathInfo.Instance).
				Msg("Skipping per-core instance of system metric")
			continue
		}

		if pathInfo.Instance != "" && pathInfo.Instance != "_Total" && !isSystemMetric {
			// True per-core metrics: use the core number
			w.logger.Debug().Str("instance", pathInfo.Instance).Msg("Adding per-core tag")
			metricTags = append(metricTags, tags.Tag{
				Key:     "core",
				Value:   pathInfo.Instance,
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
	if n, cerr := cpu.Counts(true); cerr == nil && n > 0 {
		countTags := append([]tags.Tag{}, baseTags...)
		countTags = append(countTags, tags.Tag{Key: "core", Value: "total"})
		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      "cpu_count",
			Timestamp: timestamp,
			Value:     float64(n),
			Tags:      countTags,
		})
	}

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
	case "context_switches":
		return "cpu_context_switches", normalizedValue
	case "idle_time":
		if instance == "_Total" {
			return "cpu_idle", normalizedValue
		}
		// Per-core idle has no definition; only the total is reported.
		return "", 0
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

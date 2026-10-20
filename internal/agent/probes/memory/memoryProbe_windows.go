// internal/agent/probes/host/memoryProbe_windows.go
//go:build windows

package memory

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// Définition des compteurs de performance pour la mémoire
var memoryCounterPaths = map[string]pdh.CounterDef{
	"memory_total": {
		Path:     "\\Memory\\Commit Limit",
		Instance: "",
	},
	"memory_available": {
		Path:     "\\Memory\\Available Bytes",
		Instance: "",
	},
	"memory_committed": {
		Path:     "\\Memory\\Committed Bytes",
		Instance: "",
	},
	"memory_modified_page_list": {
		Path:     "\\Memory\\Modified Page List Bytes",
		Instance: "",
	},
	"memory_nonpaged_pool": {
		Path:     "\\Memory\\Pool Nonpaged Bytes",
		Instance: "",
	},
	"memory_paged_pool": {
		Path:     "\\Memory\\Pool Paged Bytes",
		Instance: "",
	},
	"memory_cache": {
		Path:     "\\Memory\\Cache Bytes",
		Instance: "",
	},
	"memory_page_faults": {
		Path:     "\\Memory\\Page Faults/sec",
		Instance: "",
	},
	"memory_pages_input": {
		Path:     "\\Memory\\Pages Input/sec",
		Instance: "",
	},
	"memory_pages_output": {
		Path:     "\\Memory\\Pages Output/sec",
		Instance: "",
	},
	"pagefile_usage": {
		Path:     "\\Paging File(_Total)\\% Usage",
		Instance: "",
	},
	"pagefile_usage_peak": {
		Path:     "\\Paging File(_Total)\\% Usage Peak",
		Instance: "",
	},
}

// MemoryMetrics contient toutes les métriques mémoire collectées
type MemoryMetrics struct {
	metrics map[string]float64
}

// NewMemoryMetrics crée une nouvelle instance de MemoryMetrics
func NewMemoryMetrics() *MemoryMetrics {
	return &MemoryMetrics{
		metrics: make(map[string]float64),
	}
}

// SetMetric définit la valeur d'une métrique
func (m *MemoryMetrics) SetMetric(name string, value float64) {
	m.metrics[name] = value
}

// GetMetric récupère la valeur d'une métrique
func (m *MemoryMetrics) GetMetric(name string) float64 {
	return m.metrics[name]
}

type windowsMemoryCollector struct {
	query       *pdh.Query
	paths       map[string]pdh.CounterDef
	mu          sync.Mutex
	initialized bool
	logger      *logger.ModuleLogger
}

func newMemoryCollector(config map[string]interface{}, baseLogger *logger.Logger) (hostpoll.Collector, error) {
	// Initialize PDH logger
	pdh.InitializePDHLogger(baseLogger)

	// Create module logger for host probes
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.host")

	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %w", err)
	}

	collector := &windowsMemoryCollector{
		query:  query,
		paths:  make(map[string]pdh.CounterDef),
		logger: moduleLogger,
	}

	if err := collector.initializeCounters(); err != nil {
		collector.Close()
		return nil, err
	}

	// PDH rate counters have no sample until PdhCollectQueryData has run
	// twice, so PDH_NO_DATA here is expected on a freshly-booted host or a
	// cold CI runner — failing construction on it broke probe startup
	// (#590). Collect primes the query again before reading values.
	if err := query.Collect(); err != nil {
		if !errors.Is(err, pdh.ErrNoData) {
			collector.Close()
			return nil, fmt.Errorf("failed initial collection: %w", err)
		}
		moduleLogger.Warn().Err(err).Msg("No PDH sample available at construction, deferring priming to first collection")
	}

	return collector, nil
}

func (w *windowsMemoryCollector) initializeCounters() error {
	w.logger.Debug().Msg("Initializing Memory probe with counters")

	for metricName, def := range memoryCounterPaths {
		path := pdh.BuildCounterPath(def.Path, def.Instance)
		w.paths[metricName] = pdh.CounterDef{
			Path:     path,
			Instance: def.Instance,
		}

		w.logger.Debug().Str("metric", metricName).Str("path", path).Msg("Adding counter")
		if err := w.query.AddCounter(path); err != nil {
			return fmt.Errorf("failed to add counter %s: %w", metricName, err)
		}
	}
	return nil
}

func (w *windowsMemoryCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized {
		if err := pdh.PrimeSamples(w.query); err != nil {
			return nil, err
		}
		w.initialized = true
	}

	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %w", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}

	metrics := NewMemoryMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	for name, pathInfo := range w.paths {
		value, err := w.query.GetCounterValue(pathInfo.Path)
		if err != nil {
			w.logger.Debug().Str("metric", name).Err(err).Msg("Error getting counter value")
			continue
		}

		// Préparation des tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Ajout du tag d'instance si présent (pour la mémoire, généralement vide)
		if pathInfo.Instance != "" {
			metricTags = append(metricTags, tags.Tag{
				Key:     "instance",
				Value:   pathInfo.Instance,
				Private: false,
			})
		}

		// Stockage de la métrique
		metrics.SetMetric(name, value)

		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      name,
			Timestamp: timestamp,
			Value:     value,
			Tags:      metricTags,
		})

		w.logger.Debug().Str("metric", name).Float64("value", value).Interface("tags", metricTags).Msg("Collected metric")
	}

	// Calcul et ajout du pourcentage de mémoire utilisée
	if totalBytes := metrics.GetMetric("memory_total"); totalBytes > 0 {
		if availableBytes := metrics.GetMetric("memory_available"); availableBytes > 0 {
			usedPercent := ((totalBytes - availableBytes) / totalBytes) * 100
			dataPoints = append(dataPoints, data_store.DataPoint{
				Name:      "memory_used_percent",
				Timestamp: timestamp,
				Value:     usedPercent,
				Tags:      baseTags,
			})
		}
	}

	// The page file's size, which the Unix collector sends as the swap
	// total and the native Zabbix agent as system.swap.size[,total].
	// Windows publishes the usage as a percentage through the counters
	// above, so without this the limit those percentages apply to was
	// the one item of ours a Windows host could never fill.
	if swap, serr := mem.SwapMemory(); serr == nil && swap.Total > 0 {
		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      "swap_total",
			Timestamp: timestamp,
			Value:     float64(swap.Total),
			Tags:      baseTags,
		})
	} else if serr != nil {
		w.logger.Debug().Err(serr).Msg("Page file size unavailable")
	}

	return dataPoints, nil
}

func (w *windowsMemoryCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

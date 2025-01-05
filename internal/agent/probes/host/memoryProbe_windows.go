// internal/agent/probes/host/memoryProbe_windows.go
//go:build windows

package host

import (
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// Définition des compteurs de performance pour la mémoire
var memoryCounterPaths = map[string]MetricDefinition{
	"memory_total": {
		path:     "\\Memory\\Commit Limit",
		instance: "",
	},
	"memory_available": {
		path:     "\\Memory\\Available Bytes",
		instance: "",
	},
	"memory_committed": {
		path:     "\\Memory\\Committed Bytes",
		instance: "",
	},
	"memory_modified_page_list": {
		path:     "\\Memory\\Modified Page List Bytes",
		instance: "",
	},
	"memory_nonpaged_pool": {
		path:     "\\Memory\\Pool Nonpaged Bytes",
		instance: "",
	},
	"memory_paged_pool": {
		path:     "\\Memory\\Pool Paged Bytes",
		instance: "",
	},
	"memory_cache": {
		path:     "\\Memory\\Cache Bytes",
		instance: "",
	},
	"memory_page_faults": {
		path:     "\\Memory\\Page Faults/sec",
		instance: "",
	},
	"memory_pages_input": {
		path:     "\\Memory\\Pages Input/sec",
		instance: "",
	},
	"memory_pages_output": {
		path:     "\\Memory\\Pages Output/sec",
		instance: "",
	},
	"pagefile_usage": {
		path:     "\\Paging File(_Total)\\% Usage",
		instance: "",
	},
	"pagefile_usage_peak": {
		path:     "\\Paging File(_Total)\\% Usage Peak",
		instance: "",
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
	paths       map[string]pathInfo
	mu          sync.Mutex
	initialized bool
}

func newMemoryCollector(config map[string]interface{}, logger *logger.Logger) (osCollector, error) {
	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	collector := &windowsMemoryCollector{
		query: query,
		paths: make(map[string]pathInfo),
	}

	if err := collector.initializeCounters(); err != nil {
		collector.Close()
		return nil, err
	}

	if err := query.Collect(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed initial collection: %v", err)
	}

	return collector, nil
}

func (w *windowsMemoryCollector) initializeCounters() error {
	fmt.Printf("Initializing Memory probe with counters\n")

	for metricName, def := range memoryCounterPaths {
		path := pdh.BuildCounterPath(def.path, def.instance)
		w.paths[metricName] = pathInfo{
			path:     path,
			instance: def.instance,
		}

		fmt.Printf("Adding counter %s with path: %s\n", metricName, path)
		if err := w.query.AddCounter(path); err != nil {
			return fmt.Errorf("failed to add counter %s: %v", metricName, err)
		}
	}
	return nil
}

func (w *windowsMemoryCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.initialized {
		if err := w.query.Collect(); err != nil {
			return nil, fmt.Errorf("failed initial sample collection: %v", err)
		}
		time.Sleep(1 * time.Second)
		w.initialized = true
	}

	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %v", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}

	metrics := NewMemoryMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	for name, pathInfo := range w.paths {
		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			fmt.Printf("Error getting counter value for %s: %v\n", name, err)
			continue
		}

		// Préparation des tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Ajout du tag d'instance si présent (pour la mémoire, généralement vide)
		if pathInfo.instance != "" {
			metricTags = append(metricTags, tags.Tag{
				Key:     "instance",
				Value:   pathInfo.instance,
				Private: false,
			})
		}

		// Stockage de la métrique
		metrics.SetMetric(name, value)

		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      name,
			Timestamp: timestamp,
			Value:     float32(value),
			Tags:      metricTags,
		})

		fmt.Printf("Collected metric %s = %f, tags: %v\n", name, value, metricTags)
	}

	// Calcul et ajout du pourcentage de mémoire utilisée
	if totalBytes := metrics.GetMetric("memory_total"); totalBytes > 0 {
		if availableBytes := metrics.GetMetric("memory_available"); availableBytes > 0 {
			usedPercent := ((totalBytes - availableBytes) / totalBytes) * 100
			dataPoints = append(dataPoints, data_store.DataPoint{
				Name:      "memory_used_percent",
				Timestamp: timestamp,
				Value:     float32(usedPercent),
				Tags:      baseTags,
			})
		}
	}

	return dataPoints, nil
}

func (w *windowsMemoryCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

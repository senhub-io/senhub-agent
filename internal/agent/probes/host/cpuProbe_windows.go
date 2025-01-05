//go:build windows

package host

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// Configuration des filtres d'instances
var instanceFilters = struct {
	Include []string
	Exclude []string
}{
	Include: []string{}, // Liste des instances à inclure (vide = toutes)
	Exclude: []string{}, // Liste des instances à exclure
}

// MetricDefinition définit un compteur de performance avec son chemin et son instance
type MetricDefinition struct {
	path     string
	instance string
}

// Définition des compteurs de performance
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

// CPUMetrics contient toutes les métriques CPU collectées
type CPUMetrics struct {
	metrics map[string]float64
}

// NewCPUMetrics crée une nouvelle instance de CPUMetrics
func NewCPUMetrics() *CPUMetrics {
	return &CPUMetrics{
		metrics: make(map[string]float64),
	}
}

// SetMetric définit la valeur d'une métrique
func (c *CPUMetrics) SetMetric(name string, value float64) {
	c.metrics[name] = value
}

// GetMetric récupère la valeur d'une métrique
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
}

func newCollector(config map[string]interface{}, logger *logger.Logger) (osCollector, error) {
	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	collector := &windowsCollector{
		query: query,
		paths: make(map[string]pathInfo),
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

// shouldIncludeInstance vérifie si une instance doit être incluse selon les filtres
func (w *windowsCollector) shouldIncludeInstance(instance string) bool {
	// Si la liste d'inclusion est vide, tout est inclus par défaut
	isIncluded := len(instanceFilters.Include) == 0

	// Si la liste d'inclusion n'est pas vide, vérifie si l'instance y est
	for _, includedInstance := range instanceFilters.Include {
		if includedInstance == instance {
			isIncluded = true
			break
		}
	}

	// Si l'instance n'est pas incluse, pas besoin de vérifier les exclusions
	if !isIncluded {
		fmt.Printf("Instance %s not in include list\n", instance)
		return false
	}

	// Vérifie si l'instance est dans la liste d'exclusion
	for _, excludedInstance := range instanceFilters.Exclude {
		if excludedInstance == instance {
			fmt.Printf("Instance %s found in exclude list\n", instance)
			return false
		}
	}

	return true
}

func (w *windowsCollector) initializeCounters() error {
	fmt.Printf("Initializing CPU probe with counters\n")

	for metricName, def := range counterPaths {
		if def.instance == "*" {
			parts := strings.Split(def.path, "\\")
			if len(parts) < 3 {
				return fmt.Errorf("invalid counter path format: %s", def.path)
			}
			objectName := parts[1]

			// Obtenir toutes les instances, y compris _Total
			instances, err := pdh.GetInstancesList(objectName, false)
			if err != nil {
				return fmt.Errorf("failed to get %s instances: %v", objectName, err)
			}

			// Ajouter _Total à la liste des instances s'il n'y est pas déjà
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
				// Applique les filtres d'instance
				if !w.shouldIncludeInstance(instance) {
					fmt.Printf("Skipping instance %s due to filters\n", instance)
					continue
				}

				path := pdh.BuildCounterPath(def.path, instance)
				w.paths[metricName] = pathInfo{
					path:     path,
					instance: instance,
				}

				fmt.Printf("Adding counter %s with path: %s (instance: %s)\n", metricName, path, instance)
				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (instance %s): %v", metricName, instance, err)
				}
			}
		} else {
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
	}
	return nil
}

func (w *windowsCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
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

	metrics := NewCPUMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	for name, pathInfo := range w.paths {
		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			fmt.Printf("Error getting counter value for %s: %v\n", name, err)
			continue
		}

		// Préparation des tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Ajout du tag d'instance si présent
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

	return dataPoints, nil
}

func (w *windowsCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

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

func newCPUCollector(config map[string]interface{}, logger *logger.Logger) (osCollector, error) {
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
// Amélioration de shouldIncludeInstance
func (w *windowsCollector) shouldIncludeInstance(instance string) bool {
	fmt.Printf("Checking instance '%s' against filters - Include: %v, Exclude: %v\n",
		instance, instanceFilters.Include, instanceFilters.Exclude)

	// Si la liste d'inclusion est vide, tout est inclus par défaut
	if len(instanceFilters.Include) == 0 {
		// Vérifier uniquement les exclusions
		for _, excludedInstance := range instanceFilters.Exclude {
			if excludedInstance == instance {
				fmt.Printf("Instance '%s' excluded by filter\n", instance)
				return false
			}
		}
		fmt.Printf("Instance '%s' included (no include filters, not in exclude list)\n", instance)
		return true
	}

	// Si la liste d'inclusion n'est pas vide, vérifie si l'instance y est
	for _, includedInstance := range instanceFilters.Include {
		if includedInstance == instance {
			// Vérifier que l'instance n'est pas dans la liste d'exclusion
			for _, excludedInstance := range instanceFilters.Exclude {
				if excludedInstance == instance {
					fmt.Printf("Instance '%s' found in include list but excluded\n", instance)
					return false
				}
			}
			fmt.Printf("Instance '%s' found in include list and not excluded\n", instance)
			return true
		}
	}

	fmt.Printf("Instance '%s' not in include list\n", instance)
	return false
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
					fmt.Printf("Instance %s skipped due to filters\n", instance)
					continue
				}

				path := pdh.BuildCounterPath(def.path, instance)
				// Création d'une clé unique pour chaque combinaison métrique/instance
				uniqueKey := fmt.Sprintf("%s|%s", metricName, instance)
				w.paths[uniqueKey] = pathInfo{
					path:     path,
					instance: instance,
				}

				fmt.Printf("Adding counter %s with path: %s (instance: %s)\n",
					metricName, path, instance)
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

	fmt.Printf("\nStarting metrics collection at %v\n", timestamp)
	fmt.Printf("Number of paths to collect: %d\n", len(w.paths))

	// Afficher tous les chemins enregistrés
	fmt.Println("\nRegistered paths:")
	for name, pathInfo := range w.paths {
		fmt.Printf("- %s: path=%s, instance=%s\n", name, pathInfo.path, pathInfo.instance)
	}

	if !w.initialized {
		fmt.Println("Initializing first collection...")
		if err := w.query.Collect(); err != nil {
			return nil, fmt.Errorf("failed initial sample collection: %v", err)
		}
		time.Sleep(1 * time.Second)
		w.initialized = true
		fmt.Println("First collection initialized")
	}

	fmt.Println("\nCollecting metrics...")
	if err := w.query.Collect(); err != nil {
		return nil, fmt.Errorf("failed to collect PDH metrics: %v", err)
	}

	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %v", err)
	}
	fmt.Printf("Base tags: %v\n", baseTags)

	metrics := NewCPUMetrics()
	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	fmt.Println("\nProcessing individual metrics:")
	for name, pathInfo := range w.paths {
		fmt.Printf("\nProcessing metric '%s' with path '%s'\n", name, pathInfo.path)

		metricName := strings.Split(name, "|")[0]

		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			fmt.Printf("Error getting counter value for %s: %v\n", name, err)
			continue
		}

		// Préparation des tags
		metricTags := append([]tags.Tag{}, baseTags...)

		// Ajout du tag d'instance si présent
		if pathInfo.instance != "" {
			fmt.Printf("Adding instance tag: %s\n", pathInfo.instance)
			metricTags = append(metricTags, tags.Tag{
				Key:     "instance",
				Value:   pathInfo.instance,
				Private: false,
			})
		}

		// Stockage de la métrique
		metrics.SetMetric(name, value)

		dataPoint := data_store.DataPoint{
			Name:      metricName, // Utiliser metricName au lieu de strings.Split(name, "_")[0]
			Timestamp: timestamp,
			Value:     float32(value),
			Tags:      metricTags,
		}
		dataPoints = append(dataPoints, dataPoint)

		fmt.Printf("Collected metric %s = %f\n", name, value)
		fmt.Printf("Tags for this metric: %v\n", metricTags)
	}

	fmt.Printf("\nCollection completed. Total metrics collected: %d\n", len(dataPoints))
	return dataPoints, nil
}

func (w *windowsCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

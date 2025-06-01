//go:build windows

package host

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/windows/pdh"
)

// Configuration des filtres de disques
var driveFilters = struct {
	Include []string
	Exclude []string
}{
	Include: []string{},                            // Liste des disques à inclure (vide = tous)
	Exclude: []string{"HarddiskVolume*", "_Total"}, // Liste des disques à exclure
}

// MetricDefinition définit un compteur de performance avec son chemin
type LogicalDiskMetricDefinition struct {
	path     string
	instance string
}

// Définition des compteurs de performance
var logicaldiskCounterPaths = map[string]LogicalDiskMetricDefinition{
	"disk_free_mb": {
		path:     "\\LogicalDisk\\Free Megabytes",
		instance: "*",
	},
	"disk_free_percent": {
		path:     "\\LogicalDisk\\% Free Space",
		instance: "*",
	},
	"disk_reads_sec": {
		path:     "\\LogicalDisk\\Disk Reads/sec",
		instance: "*",
	},
	"disk_writes_sec": {
		path:     "\\LogicalDisk\\Disk Writes/sec",
		instance: "*",
	},
	"disk_read_bytes_sec": {
		path:     "\\LogicalDisk\\Disk Read Bytes/sec",
		instance: "*",
	},
	"disk_write_bytes_sec": {
		path:     "\\LogicalDisk\\Disk Write Bytes/sec",
		instance: "*",
	},
	"disk_queue_length": {
		path:     "\\LogicalDisk\\Current Disk Queue Length",
		instance: "*",
	},
}

type windowsLogicalDiskCollector struct {
	query          *pdh.Query
	paths          map[string]pathInfo
	initialized    bool
	includeFilters []string
	excludeFilters []string
	logger         *logger.ModuleLogger
}

func newLogicalDiskCollector(config map[string]interface{}, baseLogger *logger.Logger) (logicaldiskCollector, error) {
	// Initialize PDH logger
	pdh.InitializePDHLogger(baseLogger)
	
	// Create module logger for host probes
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.host")
	
	query, err := pdh.NewQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to create PDH query: %v", err)
	}

	collector := &windowsLogicalDiskCollector{
		query:          query,
		paths:          make(map[string]pathInfo),
		includeFilters: make([]string, len(driveFilters.Include)),
		excludeFilters: make([]string, len(driveFilters.Exclude)),
		logger:         moduleLogger,
	}

	// Copie des filtres par défaut
	copy(collector.includeFilters, driveFilters.Include)
	copy(collector.excludeFilters, driveFilters.Exclude)

	// Override des filtres depuis la configuration si spécifié
	if filters, ok := config["filters"].(map[string]interface{}); ok {
		if include, ok := filters["include"].([]string); ok {
			collector.includeFilters = include
		}
		if exclude, ok := filters["exclude"].([]string); ok {
			collector.excludeFilters = exclude
		}
	}

	collector.logger.Debug().
		Strs("include_filters", collector.includeFilters).
		Strs("exclude_filters", collector.excludeFilters).
		Msg("Initializing logical disk collector with filters")

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

// shouldIncludeDrive vérifie si un disque doit être inclus selon les filtres
func (w *windowsLogicalDiskCollector) shouldIncludeDrive(drive string) bool {
	// Si la liste d'inclusion est vide, tout est inclus par défaut
	isIncluded := len(w.includeFilters) == 0

	// Si la liste d'inclusion n'est pas vide, vérifie si le disque correspond à un pattern
	for _, pattern := range w.includeFilters {
		matched, err := filepath.Match(pattern, drive)
		if err != nil {
			w.logger.Debug().Str("pattern", pattern).Err(err).Msg("Invalid include pattern")
			continue
		}
		if matched {
			isIncluded = true
			break
		}
	}

	// Si le disque n'est pas inclus, pas besoin de vérifier les exclusions
	if !isIncluded {
		w.logger.Debug().Str("drive", drive).Msg("Drive does not match any include patterns")
		return false
	}

	// Vérifie si le disque correspond à un pattern d'exclusion
	for _, pattern := range w.excludeFilters {
		matched, err := filepath.Match(pattern, drive)
		if err != nil {
			w.logger.Debug().Str("pattern", pattern).Err(err).Msg("Invalid exclude pattern")
			continue
		}
		if matched {
			w.logger.Debug().Str("drive", drive).Str("pattern", pattern).Msg("Drive matches exclude pattern")
			return false
		}
	}

	return true
}

func (w *windowsLogicalDiskCollector) initializeCounters() error {
	w.logger.Debug().Msg("Initializing logical disk probe with counters")

	for metricName, def := range logicaldiskCounterPaths {
		if def.instance == "*" {
			// Obtenir toutes les instances de disques logiques
			instances, err := pdh.GetInstancesList("LogicalDisk", false)
			if err != nil {
				return fmt.Errorf("failed to get LogicalDisk instances: %v", err)
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
				// Applique les filtres de disques
				if !w.shouldIncludeDrive(instance) {
					w.logger.Debug().Str("drive", instance).Msg("Skipping drive due to filters")
					continue
				}

				path := pdh.BuildCounterPath(def.path, instance)
				w.paths[fmt.Sprintf("%s_%s", metricName, instance)] = pathInfo{
					path:     path,
					instance: instance,
				}

				w.logger.Debug().Str("metric", metricName).Str("path", path).Str("drive", instance).Msg("Adding counter")
				if err := w.query.AddCounter(path); err != nil {
					return fmt.Errorf("failed to add counter %s (drive %s): %v", metricName, instance, err)
				}
			}
		}
	}
	return nil
}

func (w *windowsLogicalDiskCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
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

	dataPoints := make([]data_store.DataPoint, 0, len(w.paths))

	for name, pathInfo := range w.paths {
		value, err := w.query.GetCounterValue(pathInfo.path)
		if err != nil {
			w.logger.Debug().Str("metric", name).Err(err).Msg("Error getting counter value")
			continue
		}

		// Préparation des tags
		metricTags := append([]tags.Tag{}, baseTags...)
		if pathInfo.instance != "" && pathInfo.instance != "_Total" {
			metricTags = append(metricTags, tags.Tag{
				Key:   "drive",
				Value: pathInfo.instance,
			})
		}

		// Construction du nom de la métrique en retirant l'instance
		metricName := name
		if idx := strings.LastIndex(name, "_"); idx != -1 {
			metricName = name[:idx]
		}

		dataPoints = append(dataPoints, data_store.DataPoint{
			Name:      metricName,
			Timestamp: timestamp,
			Value:     float32(value),
			Tags:      metricTags,
		})

		w.logger.Debug().Str("metric", metricName).Float64("value", value).Interface("tags", metricTags).Msg("Collected metric")
	}

	return dataPoints, nil
}

func (w *windowsLogicalDiskCollector) Close() error {
	if w.query != nil {
		w.query.Close()
	}
	return nil
}

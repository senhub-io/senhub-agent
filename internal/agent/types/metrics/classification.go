// Package metrics provides standardized metrics classification and metadata management
package metrics

import (
	"fmt"
	"time"
	
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// MetricCategory represents the primary classification of a metric
type MetricCategory string

const (
	// Health represents availability, connectivity, and system status metrics
	CategoryHealth MetricCategory = "health"
	
	// Performance represents speed, responsiveness, and throughput metrics
	CategoryPerformance MetricCategory = "performance"
	
	// Capacity represents resource utilization and consumption metrics
	CategoryCapacity MetricCategory = "capacity"
	
	// Quality represents reliability, consistency, and error metrics
	CategoryQuality MetricCategory = "quality"
	
	// Security represents access control, authentication, and vulnerability metrics
	CategorySecurity MetricCategory = "security"
	
	// Business represents SLA compliance, cost, and business impact metrics
	CategoryBusiness MetricCategory = "business"
)

// MetricSubcategory provides more granular classification within each category
type MetricSubcategory string

// Health subcategories
const (
	SubcategoryAvailability    MetricSubcategory = "availability"     // up/down status, reachability
	SubcategoryConnectivity    MetricSubcategory = "connectivity"     // ping response, network connectivity
	SubcategoryServiceStatus   MetricSubcategory = "service_status"   // process running, service health checks
	SubcategorySystemHealth    MetricSubcategory = "system_health"    // overall system status
)

// Performance subcategories
const (
	SubcategoryResponseTime    MetricSubcategory = "response_time"    // network latency, application response time
	SubcategoryThroughput      MetricSubcategory = "throughput"       // network throughput, disk IOPS
	SubcategoryProcessingSpeed MetricSubcategory = "processing_speed" // processing time, CPU speed
)

// Capacity subcategories
const (
	SubcategoryCPU     MetricSubcategory = "cpu"     // utilization, load average
	SubcategoryMemory  MetricSubcategory = "memory"  // RAM used/free, swap usage
	SubcategoryStorage MetricSubcategory = "storage" // disk space used/free, inodes
	SubcategoryNetwork MetricSubcategory = "network" // bandwidth used/available
)

// Quality subcategories
const (
	SubcategoryErrorRates  MetricSubcategory = "error_rates"  // system/network error rates
	SubcategoryStability   MetricSubcategory = "stability"    // uptime, MTBF, restart frequency
	SubcategoryConsistency MetricSubcategory = "consistency"  // performance variance
)

// Security subcategories
const (
	SubcategoryAuthentication MetricSubcategory = "authentication"  // login failures
	SubcategoryAccessControl  MetricSubcategory = "access_control"  // unauthorized access attempts
	SubcategoryVulnerability  MetricSubcategory = "vulnerability"   // security update status
)

// Business subcategories
const (
	SubcategorySLACompliance MetricSubcategory = "sla_compliance" // SLA adherence metrics
	SubcategoryCost          MetricSubcategory = "cost"           // resource cost metrics
	SubcategoryUserImpact    MetricSubcategory = "user_impact"    // affected users count
)

// MetricSeverity indicates the criticality level of a metric
type MetricSeverity string

const (
	SeverityCritical MetricSeverity = "critical" // System-critical metrics that require immediate attention
	SeverityHigh     MetricSeverity = "high"     // Important metrics that need attention
	SeverityMedium   MetricSeverity = "medium"   // Standard monitoring metrics
	SeverityLow      MetricSeverity = "low"      // Informational metrics
	SeverityInfo     MetricSeverity = "info"     // Metadata and configuration metrics
)

// MetricUnit represents the unit of measurement for a metric
type MetricUnit string

const (
	// Basic units
	UnitNone        MetricUnit = ""           // Dimensionless values (ratios, percentages as decimals)
	UnitPercent     MetricUnit = "percent"    // Percentage values (0-100)
	UnitCount       MetricUnit = "count"      // Simple counts and quantities
	UnitBoolean     MetricUnit = "boolean"    // Boolean values (0/1)
	
	// Time units
	UnitSeconds      MetricUnit = "seconds"      // Time duration in seconds
	UnitMilliseconds MetricUnit = "milliseconds" // Time duration in milliseconds
	UnitMicroseconds MetricUnit = "microseconds" // Time duration in microseconds
	
	// Byte units
	UnitBytes      MetricUnit = "bytes"      // Storage/memory in bytes
	UnitKilobytes  MetricUnit = "kilobytes"  // Storage/memory in KB
	UnitMegabytes  MetricUnit = "megabytes"  // Storage/memory in MB
	UnitGigabytes  MetricUnit = "gigabytes"  // Storage/memory in GB
	UnitTerabytes  MetricUnit = "terabytes"  // Storage/memory in TB
	
	// Rate units
	UnitBytesPerSecond MetricUnit = "bytes_per_second" // Data transfer rates
	UnitOpsPerSecond   MetricUnit = "ops_per_second"   // Operations per second
	UnitRequestsPerSec MetricUnit = "requests_per_sec" // Requests per second
	
	// Hardware units
	UnitCelsius    MetricUnit = "celsius"    // Temperature in Celsius
	UnitFahrenheit MetricUnit = "fahrenheit" // Temperature in Fahrenheit
	UnitWatts      MetricUnit = "watts"      // Power consumption in watts
	UnitVolts      MetricUnit = "volts"      // Electrical voltage
	UnitAmps       MetricUnit = "amps"       // Electrical current
	UnitRPM        MetricUnit = "rpm"        // Rotational speed (fans, disks)
)

// MetricClassification provides complete classification metadata for a metric
type MetricClassification struct {
	Category    MetricCategory    `json:"category"`    // Primary category (health, performance, etc.)
	Subcategory MetricSubcategory `json:"subcategory"` // Subcategory within the primary category
	Severity    MetricSeverity    `json:"severity"`    // Criticality level
	Unit        MetricUnit        `json:"unit"`        // Unit of measurement
	Description string            `json:"description"` // Human-readable description
	
	// Threshold information for alerting and visualization
	Thresholds *MetricThresholds `json:"thresholds,omitempty"`
	
	// Metadata for UI and dashboards
	DisplayName   string            `json:"display_name"`   // User-friendly display name
	Group         string            `json:"group"`          // Logical grouping for UI
	SortOrder     int               `json:"sort_order"`     // Display order within group
	Tags          map[string]string `json:"tags"`           // Additional classification tags
}

// MetricThresholds defines alerting and visualization thresholds
type MetricThresholds struct {
	Critical *ThresholdRange `json:"critical,omitempty"` // Critical alert thresholds
	Warning  *ThresholdRange `json:"warning,omitempty"`  // Warning alert thresholds
	Normal   *ThresholdRange `json:"normal,omitempty"`   // Normal operating range
	
	// For capacity metrics
	CapacityWarning  *float64 `json:"capacity_warning,omitempty"`  // Capacity warning threshold (percentage)
	CapacityCritical *float64 `json:"capacity_critical,omitempty"` // Capacity critical threshold (percentage)
}

// ThresholdRange defines a range with optional min/max bounds
type ThresholdRange struct {
	Min *float64 `json:"min,omitempty"` // Minimum threshold (inclusive)
	Max *float64 `json:"max,omitempty"` // Maximum threshold (inclusive)
}

// ClassifiedMetric represents a metric with its classification information
type ClassifiedMetric struct {
	// Core metric data
	DataPoint data_store.DataPoint `json:"datapoint"`
	
	// Classification metadata
	Classification MetricClassification `json:"classification"`
	
	// Collection metadata
	CollectedAt   time.Time `json:"collected_at"`   // When the metric was collected
	Source        string    `json:"source"`         // Source probe/collector
	SourceVersion string    `json:"source_version"` // Version of the source
}

// MetricClassifier interface defines how probes should classify their metrics
type MetricClassifier interface {
	// ClassifyMetric assigns classification to a metric based on its name and characteristics
	ClassifyMetric(metricName string, value float64, tags []tags.Tag) MetricClassification
	
	// GetSupportedCategories returns the categories this classifier supports
	GetSupportedCategories() []MetricCategory
	
	// GetCategoryMetrics returns all metrics for a given category
	GetCategoryMetrics(category MetricCategory) []string
}

// ClassificationRegistry manages metric classifiers for different probe types
type ClassificationRegistry struct {
	classifiers map[string]MetricClassifier // probe name -> classifier
	logger      *logger.ModuleLogger        // Module-specific logger
}

// NewClassificationRegistry creates a new classification registry
func NewClassificationRegistry(baseLogger *logger.Logger) *ClassificationRegistry {
	moduleLogger := logger.NewModuleLogger(baseLogger, "metrics.classification")
	
	registry := &ClassificationRegistry{
		classifiers: make(map[string]MetricClassifier),
		logger:      moduleLogger,
	}
	
	registry.logger.Info().Msg("Classification registry initialized")
	return registry
}

// RegisterClassifier registers a classifier for a specific probe type
func (r *ClassificationRegistry) RegisterClassifier(probeName string, classifier MetricClassifier) {
	r.classifiers[probeName] = classifier
	r.logger.Info().
		Str("probe", probeName).
		Strs("categories", stringifyCategories(classifier.GetSupportedCategories())).
		Msg("Registered classifier for probe")
}

// GetClassifier returns the classifier for a probe type
func (r *ClassificationRegistry) GetClassifier(probeName string) (MetricClassifier, bool) {
	classifier, exists := r.classifiers[probeName]
	return classifier, exists
}

// ClassifyMetric classifies a metric using the appropriate probe classifier
func (r *ClassificationRegistry) ClassifyMetric(probeName, metricName string, value float64, tags []tags.Tag) (*MetricClassification, error) {
	classifier, exists := r.classifiers[probeName]
	if !exists {
		r.logger.Debug().
			Str("probe", probeName).
			Str("metric", metricName).
			Msg("No classifier found for probe, using default classification")
		
		// Return default classification if no specific classifier exists
		return &MetricClassification{
			Category:    CategoryHealth,
			Subcategory: SubcategorySystemHealth,
			Severity:    SeverityMedium,
			Unit:        UnitNone,
			Description: "Unclassified metric",
			DisplayName: metricName,
			Group:       "General",
			SortOrder:   999,
		}, nil
	}
	
	classification := classifier.ClassifyMetric(metricName, value, tags)
	
	r.logger.Debug().
		Str("probe", probeName).
		Str("metric", metricName).
		Str("category", string(classification.Category)).
		Str("subcategory", string(classification.Subcategory)).
		Str("severity", string(classification.Severity)).
		Msg("Metric classified")
	
	return &classification, nil
}

// GetAllCategories returns all available metric categories
func GetAllCategories() []MetricCategory {
	return []MetricCategory{
		CategoryHealth,
		CategoryPerformance, 
		CategoryCapacity,
		CategoryQuality,
		CategorySecurity,
		CategoryBusiness,
	}
}

// GetCategorySubcategories returns all subcategories for a given category
func GetCategorySubcategories(category MetricCategory) []MetricSubcategory {
	switch category {
	case CategoryHealth:
		return []MetricSubcategory{
			SubcategoryAvailability,
			SubcategoryConnectivity,
			SubcategoryServiceStatus,
			SubcategorySystemHealth,
		}
	case CategoryPerformance:
		return []MetricSubcategory{
			SubcategoryResponseTime,
			SubcategoryThroughput,
			SubcategoryProcessingSpeed,
		}
	case CategoryCapacity:
		return []MetricSubcategory{
			SubcategoryCPU,
			SubcategoryMemory,
			SubcategoryStorage,
			SubcategoryNetwork,
		}
	case CategoryQuality:
		return []MetricSubcategory{
			SubcategoryErrorRates,
			SubcategoryStability,
			SubcategoryConsistency,
		}
	case CategorySecurity:
		return []MetricSubcategory{
			SubcategoryAuthentication,
			SubcategoryAccessControl,
			SubcategoryVulnerability,
		}
	case CategoryBusiness:
		return []MetricSubcategory{
			SubcategorySLACompliance,
			SubcategoryCost,
			SubcategoryUserImpact,
		}
	default:
		return []MetricSubcategory{}
	}
}

// ValidateClassification ensures a classification is valid
func ValidateClassification(classification MetricClassification) error {
	// Validate category exists
	validCategories := GetAllCategories()
	categoryValid := false
	for _, cat := range validCategories {
		if cat == classification.Category {
			categoryValid = true
			break
		}
	}
	if !categoryValid {
		return fmt.Errorf("invalid category: %s", classification.Category)
	}
	
	// Validate subcategory belongs to category
	validSubcategories := GetCategorySubcategories(classification.Category)
	subcategoryValid := false
	for _, subcat := range validSubcategories {
		if subcat == classification.Subcategory {
			subcategoryValid = true
			break
		}
	}
	if !subcategoryValid {
		return fmt.Errorf("invalid subcategory %s for category %s", classification.Subcategory, classification.Category)
	}
	
	return nil
}

// stringifyCategories converts MetricCategory slice to string slice for logging
func stringifyCategories(categories []MetricCategory) []string {
	result := make([]string, len(categories))
	for i, category := range categories {
		result[i] = string(category)
	}
	return result
}
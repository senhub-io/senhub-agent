# Metrics Classification System

The SenHub Agent implements a standardized metrics classification system that categorizes all metrics into six primary categories with detailed subcategories. This system provides consistent organization, alerting thresholds, and metadata for all monitoring data.

## Table of Contents

- [Overview](#overview)
- [Classification Categories](#classification-categories)
- [Architecture](#architecture)
- [Implementation](#implementation)
- [Redfish Integration](#redfish-integration)
- [Usage Examples](#usage-examples)
- [Extending the System](#extending-the-system)

## Overview

The metrics classification system provides:

- **Standardized Categories**: Six primary categories covering all monitoring aspects
- **Granular Subcategories**: Detailed classification within each primary category
- **Severity Levels**: Five severity levels for prioritization and alerting
- **Unit Standards**: Consistent units across all metrics
- **Threshold Management**: Built-in alerting thresholds and operational ranges
- **UI Organization**: Display names, groups, and sort orders for dashboards

## Classification Categories

### 1. Health (Availability & Status)

Monitors system availability, connectivity, and operational status.

**Subcategories:**
- `availability`: up/down status, reachability
- `connectivity`: ping response, network connectivity  
- `service_status`: process running, service health checks
- `system_health`: overall system status

**Examples:**
- System power state (on/off)
- Component health status (CPU, memory, drives)
- Network adapter connectivity
- Service availability

### 2. Performance (Speed & Responsiveness)

Measures system speed, responsiveness, and throughput capabilities.

**Subcategories:**
- `response_time`: network latency, application response time
- `throughput`: network throughput, disk IOPS
- `processing_speed`: processing time, CPU speed

**Examples:**
- Fan rotational speed (RPM)
- Drive read/write IOPS
- Network throughput
- Response times

### 3. Capacity (Resource Utilization)

Tracks resource consumption and utilization across system components.

**Subcategories:**
- `cpu`: utilization, load average
- `memory`: RAM used/free, swap usage
- `storage`: disk space used/free, pool capacity
- `network`: bandwidth used/available

**Examples:**
- CPU utilization percentage
- Memory usage percentage
- Storage pool capacity used
- Power consumption (watts)

### 4. Quality (Reliability & Consistency)

Measures reliability, error rates, and performance consistency.

**Subcategories:**
- `error_rates`: system/network error rates
- `stability`: uptime, MTBF, restart frequency
- `consistency`: performance variance

**Examples:**
- Drive read/write errors
- Network error counts
- System uptime
- Performance variance metrics

### 5. Security (Access & Protection)

Monitors security-related metrics and access control.

**Subcategories:**
- `authentication`: login failures
- `access_control`: unauthorized access attempts
- `vulnerability`: security update status

**Examples:**
- Failed authentication attempts
- Unauthorized access logs
- Security compliance status
- Vulnerability counts

### 6. Business (Impact & Cost)

Tracks business-related metrics, SLA compliance, and operational costs.

**Subcategories:**
- `sla_compliance`: SLA adherence metrics
- `cost`: resource cost metrics
- `user_impact`: affected users count

**Examples:**
- SLA uptime percentage
- Resource cost per hour
- Number of affected users during incidents
- Business transaction success rates

## Architecture

### Core Components

#### MetricClassification Structure

```go
type MetricClassification struct {
    Category    MetricCategory    // Primary category (health, performance, etc.)
    Subcategory MetricSubcategory // Subcategory within the primary category
    Severity    MetricSeverity    // Criticality level (critical, high, medium, low, info)
    Unit        MetricUnit        // Unit of measurement (percent, celsius, bytes, etc.)
    Description string            // Human-readable description
    
    // Threshold information for alerting
    Thresholds *MetricThresholds
    
    // UI metadata
    DisplayName   string            // User-friendly display name
    Group         string            // Logical grouping for UI
    SortOrder     int               // Display order within group
    Tags          map[string]string // Additional classification tags
}
```

#### Severity Levels

```go
const (
    SeverityCritical MetricSeverity = "critical" // System-critical, immediate attention
    SeverityHigh     MetricSeverity = "high"     // Important, needs attention
    SeverityMedium   MetricSeverity = "medium"   // Standard monitoring
    SeverityLow      MetricSeverity = "low"      // Informational
    SeverityInfo     MetricSeverity = "info"     // Metadata and configuration
)
```

#### Standard Units

```go
// Basic units
UnitNone        = ""           // Dimensionless values
UnitPercent     = "percent"    // Percentage values (0-100)
UnitCount       = "count"      // Simple counts
UnitBoolean     = "boolean"    // Boolean values (0/1)

// Time units  
UnitSeconds     = "seconds"    // Time duration
UnitMilliseconds = "milliseconds"

// Byte units
UnitBytes       = "bytes"      // Storage/memory
UnitKilobytes   = "kilobytes"
UnitMegabytes   = "megabytes"
UnitGigabytes   = "gigabytes"
UnitTerabytes   = "terabytes"

// Hardware units
UnitCelsius     = "celsius"    // Temperature
UnitWatts       = "watts"      // Power consumption
UnitRPM         = "rpm"        // Rotational speed
```

### MetricClassifier Interface

```go
type MetricClassifier interface {
    // ClassifyMetric assigns classification to a metric
    ClassifyMetric(metricName string, value float64, tags []tags.Tag) MetricClassification
    
    // GetSupportedCategories returns supported categories
    GetSupportedCategories() []MetricCategory
    
    // GetCategoryMetrics returns all metrics for a category
    GetCategoryMetrics(category MetricCategory) []string
}
```

## Implementation

### Classification Registry

The `ClassificationRegistry` manages classifiers for different probe types:

```go
registry := metrics.NewClassificationRegistry()

// Register Redfish classifier
redfishClassifier := redfish.NewRedfishMetricClassifier()
registry.RegisterClassifier("redfish", redfishClassifier)

// Classify a metric
classification, err := registry.ClassifyMetric("redfish", "redfish.system.health", 1.0, tags)
```

### Threshold Configuration

```go
type MetricThresholds struct {
    Critical *ThresholdRange // Critical alert thresholds
    Warning  *ThresholdRange // Warning alert thresholds
    Normal   *ThresholdRange // Normal operating range
    
    // Capacity-specific thresholds
    CapacityWarning  *float64 // Capacity warning (percentage)
    CapacityCritical *float64 // Capacity critical (percentage)
}

type ThresholdRange struct {
    Min *float64 // Minimum threshold (inclusive)
    Max *float64 // Maximum threshold (inclusive)
}
```

## Redfish Integration

### Automatic Classification

The Redfish probe automatically classifies all metrics:

```go
// In redfishProbe.Collect()
classification := p.classifier.ClassifyMetric(datapoint.Name, datapoint.Value, datapoint.Tags)

// Add classification as tags
datapoint.Tags = append(datapoint.Tags, []tags.Tag{
    {Key: "metric_category", Value: string(classification.Category)},
    {Key: "metric_subcategory", Value: string(classification.Subcategory)},
    {Key: "metric_severity", Value: string(classification.Severity)},
    {Key: "metric_unit", Value: string(classification.Unit)},
    {Key: "metric_group", Value: classification.Group},
}...)
```

### Classification Rules

The Redfish classifier includes comprehensive rules for hardware metrics:

#### System Health Examples
```go
"redfish.system.health" -> {
    Category: CategoryHealth,
    Subcategory: SubcategorySystemHealth,
    Severity: SeverityCritical,
    Unit: UnitBoolean,
    DisplayName: "System Health",
    Group: "System Status"
}

"redfish.system.power_state" -> {
    Category: CategoryHealth,
    Subcategory: SubcategoryAvailability,
    Severity: SeverityCritical,
    Unit: UnitBoolean,
    DisplayName: "Power State",
    Group: "System Status"
}
```

#### Thermal Metrics
```go
"redfish.thermal.temperature.*" -> {
    Category: CategoryHealth,
    Subcategory: SubcategorySystemHealth,
    Severity: SeverityHigh,
    Unit: UnitCelsius,
    DisplayName: "Temperature",
    Group: "Thermal",
    Thresholds: {
        Warning: {Max: 75°C},
        Critical: {Max: 85°C}
    }
}

"redfish.thermal.fan.speed.*" -> {
    Category: CategoryPerformance,
    Subcategory: SubcategoryProcessingSpeed,
    Severity: SeverityMedium,
    Unit: UnitRPM,
    DisplayName: "Fan Speed",
    Group: "Thermal"
}
```

#### Storage Capacity
```go
"redfish.storage.drive.capacity_used.*" -> {
    Category: CategoryCapacity,
    Subcategory: SubcategoryStorage,
    Severity: SeverityMedium,
    Unit: UnitPercent,
    DisplayName: "Drive Capacity Used",
    Group: "Storage",
    Thresholds: {
        Warning: {Max: 85%},
        Critical: {Max: 95%}
    }
}
```

## Usage Examples

### 1. Filtering by Category

```bash
# Get all health metrics
curl "http://localhost:8080/api/{agentkey}/metrics?category=health"

# Get all capacity metrics
curl "http://localhost:8080/api/{agentkey}/metrics?category=capacity"

# Get critical severity metrics only
curl "http://localhost:8080/api/{agentkey}/metrics?severity=critical"
```

### 2. Dashboard Organization

```javascript
// Group metrics by category for dashboard
const healthMetrics = metrics.filter(m => m.tags.metric_category === 'health');
const performanceMetrics = metrics.filter(m => m.tags.metric_category === 'performance');
const capacityMetrics = metrics.filter(m => m.tags.metric_category === 'capacity');

// Sort by group and order within group
const sortedMetrics = metrics.sort((a, b) => {
    if (a.tags.metric_group !== b.tags.metric_group) {
        return a.tags.metric_group.localeCompare(b.tags.metric_group);
    }
    return parseInt(a.tags.sort_order || 999) - parseInt(b.tags.sort_order || 999);
});
```

### 3. Alerting Rules

```yaml
# Example Prometheus alerting rules
groups:
  - name: senhub.critical
    rules:
      - alert: SystemHealthCritical
        expr: senhub_metric{metric_severity="critical", metric_category="health"} == 0
        labels:
          severity: critical
        annotations:
          summary: "Critical system health issue detected"
          
      - alert: CapacityWarning
        expr: senhub_metric{metric_category="capacity", metric_unit="percent"} > 85
        labels:
          severity: warning
        annotations:
          summary: "Capacity utilization above warning threshold"
```

### 4. Custom Classification

```go
// Implement custom classifier for a new probe type
type CustomProbeClassifier struct {
    rules map[string]metrics.MetricClassification
}

func (c *CustomProbeClassifier) ClassifyMetric(metricName string, value float64, tags []tags.Tag) metrics.MetricClassification {
    // Custom classification logic
    if strings.Contains(metricName, "response_time") {
        return metrics.MetricClassification{
            Category:    metrics.CategoryPerformance,
            Subcategory: metrics.SubcategoryResponseTime,
            Severity:    metrics.SeverityMedium,
            Unit:        metrics.UnitMilliseconds,
            DisplayName: "Response Time",
            Group:       "Application Performance",
        }
    }
    
    // Return default classification
    return c.getDefaultClassification(metricName)
}

// Register with registry
registry.RegisterClassifier("custom_probe", &CustomProbeClassifier{})
```

## Extending the System

### Adding New Categories

1. **Define the Category**:
```go
const CategoryNewCategory MetricCategory = "new_category"
```

2. **Add Subcategories**:
```go
const (
    SubcategoryNewSub1 MetricSubcategory = "new_sub_1"
    SubcategoryNewSub2 MetricSubcategory = "new_sub_2"
)
```

3. **Update Category Mapping**:
```go
func GetCategorySubcategories(category MetricCategory) []MetricSubcategory {
    switch category {
    case CategoryNewCategory:
        return []MetricSubcategory{SubcategoryNewSub1, SubcategoryNewSub2}
    // ... existing cases
    }
}
```

### Adding New Units

```go
const (
    UnitNewUnit MetricUnit = "new_unit"
)
```

### Creating Probe-Specific Classifiers

1. **Implement the Interface**:
```go
type MyProbeClassifier struct {
    // Custom fields
}

func (c *MyProbeClassifier) ClassifyMetric(metricName string, value float64, tags []tags.Tag) metrics.MetricClassification {
    // Classification logic
}

func (c *MyProbeClassifier) GetSupportedCategories() []metrics.MetricCategory {
    // Return supported categories
}

func (c *MyProbeClassifier) GetCategoryMetrics(category metrics.MetricCategory) []string {
    // Return metrics for category
}
```

2. **Register with Registry**:
```go
registry.RegisterClassifier("my_probe", &MyProbeClassifier{})
```

3. **Integrate in Probe**:
```go
// In probe's Collect method
classification := p.classifier.ClassifyMetric(datapoint.Name, datapoint.Value, datapoint.Tags)
// Add classification tags to datapoint
```

## Best Practices

1. **Consistent Naming**: Use consistent metric naming patterns that align with classification rules
2. **Appropriate Severity**: Choose severity levels based on business impact and urgency
3. **Meaningful Thresholds**: Set thresholds based on operational experience and SLA requirements
4. **Clear Display Names**: Use descriptive, user-friendly names for dashboard display
5. **Logical Grouping**: Group related metrics for better UI organization
6. **Tag Enhancement**: Combine classification with tag enhancement for maximum metadata richness

## Performance Considerations

- Classification is performed in-memory with O(1) or O(n) pattern matching
- Rules are pre-compiled at startup for optimal performance
- Classification adds minimal overhead to metric collection
- Tags are efficiently appended to existing datapoint structures

The metrics classification system provides a robust foundation for organizing, filtering, and managing monitoring data across all probe types in the SenHub Agent.
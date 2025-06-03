// senhub-agent/internal/agent/services/data_store/http_formats.go
package data_store

import (
	"fmt"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
)

// FormatConverter handles conversion between internal metrics and external monitoring tool formats
type FormatConverter struct {
	transformerRegistry *transformers.TransformerRegistry
	logger              *logger.ModuleLogger
	cache               *MetricCache
}

// NewFormatConverter creates a new format converter
func NewFormatConverter(transformerRegistry *transformers.TransformerRegistry, logger *logger.ModuleLogger, cache *MetricCache) *FormatConverter {
	return &FormatConverter{
		transformerRegistry: transformerRegistry,
		logger:              logger,
		cache:               cache,
	}
}

// SenHub Format Conversion

// GetSenHubMetricsForProbe converts cached metrics to SenHub format for a specific probe
func (f *FormatConverter) GetSenHubMetricsForProbe(probeName string) []SenHubMetric {
	f.logger.Debug().Str("probe", probeName).Msg("Converting metrics to SenHub format")
	
	// Get cached metrics for the probe
	cachedMetrics := f.cache.GetProbeMetrics(probeName)
	
	// Convert to SenHub format
	senHubMetrics := make([]SenHubMetric, 0, len(cachedMetrics))
	for _, metric := range cachedMetrics {
		senHubMetric := f.convertToSenHubFormat(metric)
		senHubMetrics = append(senHubMetrics, senHubMetric)
	}
	
	f.logger.Debug().
		Str("probe", probeName).
		Int("metrics_count", len(senHubMetrics)).
		Msg("SenHub format conversion completed")
	
	return senHubMetrics
}

// convertToSenHubFormat converts a cached metric to SenHub format
func (f *FormatConverter) convertToSenHubFormat(metric CachedMetric) SenHubMetric {
	// Get transformer for friendly name resolution
	transformer, err := f.transformerRegistry.LoadTransformer(metric.ProbeName, "friendly")
	if err != nil {
		f.logger.Warn().
			Err(err).
			Str("probe_name", metric.ProbeName).
			Msg("Failed to get transformer for SenHub format")
	}
	
	// Resolve channel name using transformer
	channelName := metric.MetricName
	if transformer != nil {
		if friendlyName := transformer.TransformMetricName(metric.MetricName, metric.Tags); friendlyName != "" {
			channelName = friendlyName
		}
	}
	
	return SenHubMetric{
		Name:        metric.MetricName,
		Channel:     channelName,
		Value:       metric.Value,
		Unit:        metric.Unit,
		Timestamp:   metric.Timestamp,
		ProbeName:   metric.ProbeName,
		Tags:        metric.Tags,
	}
}

// PRTG Format Conversion

// GetMetricsForProbe retrieves and transforms metrics for a specific probe (legacy - no filters)
func (f *FormatConverter) GetMetricsForProbe(probeName string) []PRTGChannel {
	return f.GetMetricsForProbeWithFilter(probeName, MetricFilter{})
}

// GetMetricsForProbeWithFilter retrieves and transforms metrics for a specific probe with filtering
func (f *FormatConverter) GetMetricsForProbeWithFilter(probeName string, filter MetricFilter) []PRTGChannel {
	f.logger.Debug().
		Str("probe", probeName).
		Interface("filter", filter).
		Msg("Converting metrics to PRTG format with filtering")
	
	// Get cached metrics for the probe
	cachedMetrics := f.cache.GetProbeMetrics(probeName)
	
	// Apply filters
	filteredMetrics := f.applyMetricFilter(cachedMetrics, filter)
	
	// Convert to PRTG format
	channels := make([]PRTGChannel, 0, len(filteredMetrics))
	now := time.Now()
	
	for _, metric := range filteredMetrics {
		// Skip expired metrics
		if now.Sub(metric.Timestamp) > 5*time.Minute { // TTL check
			continue
		}
		
		// Generate unique key for transformer
		tsKey := f.cache.generateTimeSeriesKey(metric.ProbeName, metric.MetricName, metric.Tags)
		
		// Transform to PRTG channel
		if channel := f.transformToPRTGChannel(tsKey, metric); channel != nil {
			channels = append(channels, *channel)
		}
	}
	
	f.logger.Debug().
		Str("probe", probeName).
		Int("filtered_metrics", len(filteredMetrics)).
		Int("prtg_channels", len(channels)).
		Msg("PRTG format conversion completed")
	
	return channels
}

// transformToPRTGChannel converts a cached metric to PRTG channel format
func (f *FormatConverter) transformToPRTGChannel(key string, metric CachedMetric) *PRTGChannel {
	// Convert value to float64
	value, ok := f.convertValueToFloat64(metric.Value)
	if !ok {
		f.logger.Warn().
			Interface("value", metric.Value).
			Str("metric", metric.MetricName).
			Msg("Failed to convert metric value to float64 for PRTG")
		return nil
	}
	
	// Transform metric name using transformer
	channelName, unit := f.transformMetricNameForPRTG(key, metric)
	
	// Format units for PRTG: use "custom" when we have a unit, put real unit in CustomUnit
	var prtgUnit, customUnit string
	if unit != "" {
		prtgUnit = "custom"
		customUnit = unit
	}
	
	return &PRTGChannel{
		Channel:    channelName,
		Value:      value,
		Float:      1,           // PRTG expects Float=1 for decimal values
		Unit:       prtgUnit,
		CustomUnit: customUnit,
	}
}

// convertValueToFloat64 safely converts metric values to float64 for PRTG
func (f *FormatConverter) convertValueToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		if floatVal, err := strconv.ParseFloat(v, 64); err == nil {
			return floatVal, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// transformMetricNameForPRTG transforms metric names using the transformer system
func (f *FormatConverter) transformMetricNameForPRTG(key string, metric CachedMetric) (string, string) {
	// Get transformer for friendly name resolution
	transformer, err := f.transformerRegistry.LoadTransformer(metric.ProbeName, "friendly")
	if err != nil {
		f.logger.Warn().
			Err(err).
			Str("probe_name", metric.ProbeName).
			Msg("Failed to get transformer for PRTG format")
		return metric.MetricName, metric.Unit
	}
	
	// Default to original values
	transformedName := metric.MetricName
	unit := metric.Unit
	
	if transformer != nil {
		// Get friendly name
		if friendlyName := transformer.TransformMetricName(metric.MetricName, metric.Tags); friendlyName != "" {
			transformedName = friendlyName
		}
		
		// Get unit from transformer only if metric doesn't have a unit
		if unit == "" {
			if transformerUnit := transformer.GetUnit(metric.MetricName); transformerUnit != "" {
				unit = transformerUnit
			}
		}
	}
	
	return transformedName, unit
}

// Metric Filtering

// applyMetricFilter applies filtering criteria to metrics
func (f *FormatConverter) applyMetricFilter(metrics []CachedMetric, filter MetricFilter) []CachedMetric {
	if filter.Limit == 0 && filter.Offset == 0 && len(filter.MetricNames) == 0 && len(filter.TagFilters) == 0 && len(filter.ExcludeTags) == 0 {
		return metrics // No filtering needed
	}
	
	filtered := make([]CachedMetric, 0)
	
	for _, metric := range metrics {
		// Filter by metric names if specified
		if len(filter.MetricNames) > 0 {
			found := false
			for _, name := range filter.MetricNames {
				if metric.MetricName == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		
		// Filter by tag filters if specified (include tags)
		if len(filter.TagFilters) > 0 {
			tagMatch := true
			for tagKey, allowedValues := range filter.TagFilters {
				metricValue, exists := metric.Tags[tagKey]
				if !exists {
					tagMatch = false
					break
				}
				// Check if metric value is in allowed values
				found := false
				for _, allowedValue := range allowedValues {
					if metricValue == allowedValue {
						found = true
						break
					}
				}
				if !found {
					tagMatch = false
					break
				}
			}
			if !tagMatch {
				continue
			}
		}
		
		// Filter by exclude tags if specified (exclude tags)
		if len(filter.ExcludeTags) > 0 {
			shouldExclude := false
			for tagKey, excludedValues := range filter.ExcludeTags {
				metricValue, exists := metric.Tags[tagKey]
				if exists {
					// Check if metric value is in excluded values
					for _, excludedValue := range excludedValues {
						if metricValue == excludedValue {
							shouldExclude = true
							break
						}
					}
					if shouldExclude {
						break
					}
				}
			}
			if shouldExclude {
				continue
			}
		}
		
		filtered = append(filtered, metric)
	}
	
	// Apply offset and limit
	start := filter.Offset
	if start > len(filtered) {
		return []CachedMetric{}
	}
	
	end := len(filtered)
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	
	return filtered[start:end]
}

// Nagios Format Conversion (basic structure - detailed implementation would be in Nagios module)

// GetNagiosMetricsForProbe converts cached metrics to basic Nagios format
func (f *FormatConverter) GetNagiosMetricsForProbe(probeName string) NagiosResponse {
	f.logger.Debug().Str("probe", probeName).Msg("Converting metrics to basic Nagios format")
	
	// Get cached metrics for the probe
	cachedMetrics := f.cache.GetProbeMetrics(probeName)
	
	// Simple conversion - for complex Nagios logic, this would delegate to a specialized module
	status := 0        // 0=OK
	statusText := "OK"
	message := fmt.Sprintf("Probe %s has %d metrics", probeName, len(cachedMetrics))
	
	if len(cachedMetrics) == 0 {
		status = 1           // 1=WARNING
		statusText = "WARNING"
		message = fmt.Sprintf("No metrics available for probe %s", probeName)
	}
	
	return NagiosResponse{
		Status:     status,
		StatusText: statusText,
		Message:    message,
		PerfData:   "",
	}
}

// Utility Functions


// senhub-agent/internal/agent/services/data_store/http_metrics.go
package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/logger"
)

// MetricsProcessor handles all metrics processing, filtering, and format conversion
type MetricsProcessor struct {
	logger          *logger.ModuleLogger
	cache           *MetricCache
	formatConverter *FormatConverter
	lookupRegistry  *LookupRegistry // Lookup definitions for status/health mappings
}

// NewMetricsProcessor creates a new metrics processing manager
func NewMetricsProcessor(cache *MetricCache, formatConverter *FormatConverter, lookupRegistry *LookupRegistry, logger *logger.ModuleLogger) *MetricsProcessor {
	return &MetricsProcessor{
		logger:          logger,
		cache:           cache,
		formatConverter: formatConverter,
		lookupRegistry:  lookupRegistry,
	}
}

// MetricFilter represents filtering criteria for metrics
type MetricFilter struct {
	TagFilters  map[string][]string // key: tag name, value: allowed values
	ExcludeTags map[string][]string // key: tag name, value: excluded values
	MetricNames []string            // specific metric names to include
	Limit       int                 // max number of results
	Offset      int                 // pagination offset
	ShowTags    bool                // whether to include tags in channel/service names (default: true)
}

// MetricExample represents an example API call for documentation
type MetricExample struct {
	Description string `json:"description"`
	URL         string `json:"url"`
	ResultCount int    `json:"estimated_results"`
}

// NagiosMetricResult represents the result of processing a single Nagios metric
type NagiosMetricResult struct {
	Status   int
	Message  string
	PerfData string
}

// SenHub Format Processing

// GetSenHubMetricsForProbe retrieves metrics for a probe in SenHub format
func (m *MetricsProcessor) GetSenHubMetricsForProbe(probeName string) []SenHubMetric {
	return m.formatConverter.GetSenHubMetricsForProbe(probeName)
}

// ConvertToSenHubFormat converts a cached metric to SenHub format
func (m *MetricsProcessor) ConvertToSenHubFormat(metric CachedMetric) SenHubMetric {
	return m.formatConverter.convertToSenHubFormat(metric)
}

// PRTG Format Processing

// GetPRTGMetricsForProbe retrieves metrics for a probe in PRTG format (no filters)
func (m *MetricsProcessor) GetPRTGMetricsForProbe(probeName string) []PRTGChannel {
	return m.formatConverter.GetMetricsForProbe(probeName)
}

// GetPRTGMetricsForProbeWithFilter retrieves and filters metrics for a probe in PRTG format
func (m *MetricsProcessor) GetPRTGMetricsForProbeWithFilter(probeName string, filter MetricFilter) []PRTGChannel {
	return m.formatConverter.GetMetricsForProbeWithFilter(probeName, filter)
}

// TransformToPRTGChannel converts a cached metric to PRTG channel format
func (m *MetricsProcessor) TransformToPRTGChannel(key string, metric CachedMetric) *PRTGChannel {
	return m.formatConverter.transformToPRTGChannel(key, metric)
}

// TransformMetricNameForPRTG transforms metric names using PRTG conventions
func (m *MetricsProcessor) TransformMetricNameForPRTG(key string, metric CachedMetric) (string, string) {
	return m.formatConverter.transformMetricNameForPRTG(key, metric)
}

// Metric Filtering and Processing

// ApplyMetricFilter applies filtering criteria to a list of metrics
func (m *MetricsProcessor) ApplyMetricFilter(metrics []CachedMetric, filter MetricFilter) []CachedMetric {
	return m.formatConverter.applyMetricFilter(metrics, filter)
}

// ParseMetricFilter parses HTTP query parameters into a MetricFilter
func (m *MetricsProcessor) ParseMetricFilter(r *http.Request) MetricFilter {
	filter := MetricFilter{
		TagFilters:  make(map[string][]string),
		ExcludeTags: make(map[string][]string),
		MetricNames: []string{},
		Limit:       0, // 0 means no limit
		Offset:      0,
		ShowTags:    true, // Default: show tags in channel/service names
	}

	query := r.URL.Query()

	// Parse tag filters (include)
	if tagParams := query["tags"]; len(tagParams) > 0 {
		for _, tagParam := range tagParams {
			m.parseTagFilter(tagParam, filter.TagFilters)
		}
	}

	// Parse exclude tag filters
	if excludeParams := query["exclude_tags"]; len(excludeParams) > 0 {
		for _, excludeParam := range excludeParams {
			m.parseTagFilter(excludeParam, filter.ExcludeTags)
		}
	}

	// Parse metric name filters
	if metricParams := query["metrics"]; len(metricParams) > 0 {
		for _, metricParam := range metricParams {
			metrics := strings.Split(metricParam, ",")
			for _, metric := range metrics {
				if metric = strings.TrimSpace(metric); metric != "" {
					filter.MetricNames = append(filter.MetricNames, metric)
				}
			}
		}
	}

	// Parse limit
	if limitParam := query.Get("limit"); limitParam != "" {
		if limit, err := strconv.Atoi(limitParam); err == nil && limit > 0 {
			filter.Limit = limit
		}
	}

	// Parse offset
	if offsetParam := query.Get("offset"); offsetParam != "" {
		if offset, err := strconv.Atoi(offsetParam); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}

	// Parse show_tags parameter (default: true)
	// Allows hiding discriminant tags from channel/service names when filtering
	// Example: ?tags=interface:LO/1&show_tags=false
	// Result: "State" instead of "Interface LO/1 - State"
	if showTagsParam := query.Get("show_tags"); showTagsParam != "" {
		if showTags, err := strconv.ParseBool(showTagsParam); err == nil {
			filter.ShowTags = showTags
		}
	}

	return filter
}

// parseTagFilter parses tag filter string like "core:0,1,2" or "interface:en0"
func (m *MetricsProcessor) parseTagFilter(param string, filterMap map[string][]string) {
	// Split only on the first colon to handle values with colons (like URLs)
	colonIndex := strings.Index(param, ":")
	if colonIndex == -1 {
		return
	}

	tagName := strings.TrimSpace(param[:colonIndex])
	valuesStr := strings.TrimSpace(param[colonIndex+1:])

	if tagName == "" || valuesStr == "" {
		return
	}

	// URL decode tag name and values
	if decodedTagName, err := url.QueryUnescape(tagName); err == nil {
		tagName = decodedTagName
	}
	if decodedValuesStr, err := url.QueryUnescape(valuesStr); err == nil {
		valuesStr = decodedValuesStr
	}

	// Split values by comma
	values := strings.Split(valuesStr, ",")
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
	}

	filterMap[tagName] = values
}

// Nagios Format Processing

// ProcessNagiosMetric processes a single metric definition against available data
func (m *MetricsProcessor) ProcessNagiosMetric(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// Filter metrics by channel name
	var matchingMetrics []CachedMetric
	for _, metric := range metrics {
		if metric.MetricName == metricDef.Channel {
			matchingMetrics = append(matchingMetrics, metric)
		}
	}

	if len(matchingMetrics) == 0 {
		return NagiosMetricResult{
			Status:   3, // UNKNOWN
			Message:  fmt.Sprintf("No metrics found for channel: %s", metricDef.Channel),
			PerfData: "",
		}
	}

	// Check if this is a health/status metric requiring special handling
	if m.isHealthStatusMetricNagios(metricDef.Channel, matchingMetrics) {
		return m.processNagiosHealthMetric(metricDef, matchingMetrics, overrides)
	}

	// Determine processing mode based on aggregation
	if metricDef.Aggregation != "" && metricDef.Aggregation != "none" {
		return m.processNagiosMetricAggregated(metricDef, matchingMetrics, overrides)
	} else {
		return m.processNagiosMetricSeparate(metricDef, matchingMetrics, overrides)
	}
}

// processNagiosMetricAggregated processes metrics with aggregation
func (m *MetricsProcessor) processNagiosMetricAggregated(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// Convert to float64 values
	var values []float64
	for _, metric := range metrics {
		if val, ok := metric.Value.(float64); ok {
			values = append(values, val)
		} else if val, ok := metric.Value.(float32); ok {
			values = append(values, float64(val))
		}
	}

	if len(values) == 0 {
		return NagiosMetricResult{
			Status:   3, // UNKNOWN
			Message:  fmt.Sprintf("No numeric values found for channel: %s", metricDef.Channel),
			PerfData: "",
		}
	}

	// Apply aggregation
	aggregatedValue := m.aggregateValues(values, metricDef.Aggregation)

	// Get thresholds (with overrides)
	warning := metricDef.Warning
	critical := metricDef.Critical
	if overrides.Warning != "" {
		warning = overrides.Warning
	}
	if overrides.Critical != "" {
		critical = overrides.Critical
	}

	// Evaluate status
	status := m.evaluateThreshold(aggregatedValue, warning, critical, metricDef.Invert)

	// Build performance data
	perfData := m.buildPerfData(metricDef.Channel, aggregatedValue, warning, critical, metricDef.Unit)

	// Build message
	statusText := m.getStatusText(status)
	message := fmt.Sprintf("%s: %s %.2f%s (aggregated from %d metrics)",
		statusText, metricDef.Channel, aggregatedValue, metricDef.Unit, len(values))

	return NagiosMetricResult{
		Status:   status,
		Message:  message,
		PerfData: perfData,
	}
}

// processNagiosMetricSeparate processes metrics separately (no aggregation)
func (m *MetricsProcessor) processNagiosMetricSeparate(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// For simplicity, return the worst case status and combine perf data
	worstStatus := 0
	var messages []string
	var perfDataItems []string

	for _, metric := range metrics {
		if val, ok := metric.Value.(float64); ok {
			// Get thresholds
			warning := metricDef.Warning
			critical := metricDef.Critical
			if overrides.Warning != "" {
				warning = overrides.Warning
			}
			if overrides.Critical != "" {
				critical = overrides.Critical
			}

			// Evaluate status for this metric
			status := m.evaluateThreshold(val, warning, critical, metricDef.Invert)
			if status > worstStatus {
				worstStatus = status
			}

			// Build context string if tag context is specified
			context := m.buildTagContext(metric, metricDef.TagContext)
			metricName := metricDef.Channel
			if context != "" {
				metricName = fmt.Sprintf("%s_%s", metricDef.Channel, context)
			}

			// Add performance data
			perfData := m.buildPerfData(metricName, val, warning, critical, metricDef.Unit)
			perfDataItems = append(perfDataItems, perfData)

			// Add status message
			statusText := m.getStatusText(status)
			if context != "" {
				messages = append(messages, fmt.Sprintf("%s[%s]: %s %.2f%s", metricDef.Channel, context, statusText, val, metricDef.Unit))
			} else {
				messages = append(messages, fmt.Sprintf("%s: %s %.2f%s", metricDef.Channel, statusText, val, metricDef.Unit))
			}
		}
	}

	return NagiosMetricResult{
		Status:   worstStatus,
		Message:  strings.Join(messages, "; "),
		PerfData: strings.Join(perfDataItems, " "),
	}
}

// ApplyNagiosTagFilters applies tag filters from Nagios check configuration
func (m *MetricsProcessor) ApplyNagiosTagFilters(metrics []CachedMetric, filters []NagiosTagFilter) []CachedMetric {
	if len(filters) == 0 {
		return metrics
	}

	var filtered []CachedMetric
	for _, metric := range metrics {
		include := true

		for _, filter := range filters {
			tagValue := m.getTagValue(metric, filter.Key)

			switch filter.Operator {
			case "in":
				if len(filter.Values) > 0 {
					found := false
					for _, allowedValue := range filter.Values {
						if tagValue == allowedValue {
							found = true
							break
						}
					}
					if !found {
						include = false
						break
					}
				}
			case "not_in":
				if len(filter.Values) > 0 {
					for _, forbiddenValue := range filter.Values {
						if tagValue == forbiddenValue {
							include = false
							break
						}
					}
				}
			case "equals":
				if len(filter.Values) > 0 && tagValue != filter.Values[0] {
					include = false
					break
				}
			case "not_equals":
				if len(filter.Values) > 0 && tagValue == filter.Values[0] {
					include = false
					break
				}
			case "exists":
				if tagValue == "" {
					include = false
					break
				}
			}
		}

		if include {
			filtered = append(filtered, metric)
		}
	}

	return filtered
}

// getTagValue gets the value of a tag from a metric
func (m *MetricsProcessor) getTagValue(metric CachedMetric, tagKey string) string {
	if value, exists := metric.Tags[tagKey]; exists {
		return value
	}
	return ""
}

// buildTagContext builds a context string from metric tags
func (m *MetricsProcessor) buildTagContext(metric CachedMetric, tagContext string) string {
	if tagContext == "" {
		return ""
	}

	tagValue := m.getTagValue(metric, tagContext)
	if tagValue != "" {
		return tagValue
	}

	return ""
}

// Utility Functions

// aggregateValues applies aggregation function to a list of values
func (m *MetricsProcessor) aggregateValues(values []float64, aggregation string) float64 {
	if len(values) == 0 {
		return 0
	}

	switch aggregation {
	case "average", "avg":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	case "max":
		max := values[0]
		for _, v := range values {
			if v > max {
				max = v
			}
		}
		return max
	case "min":
		min := values[0]
		for _, v := range values {
			if v < min {
				min = v
			}
		}
		return min
	case "sum":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum
	case "count":
		return float64(len(values))
	default:
		// Default to average
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	}
}

// evaluateThreshold evaluates a value against warning and critical thresholds
func (m *MetricsProcessor) evaluateThreshold(value float64, warning, critical string, invert bool) int {
	// Parse thresholds. An empty critical means a warn-only check
	// (standard Nagios practice — e.g. veeam_jobs_warning): the
	// metric can reach WARNING but never escalates to CRITICAL.
	warnThreshold, err := strconv.ParseFloat(warning, 64)
	if err != nil {
		return 3 // UNKNOWN
	}

	hasCritical := strings.TrimSpace(critical) != ""
	var critThreshold float64
	if hasCritical {
		critThreshold, err = strconv.ParseFloat(critical, 64)
		if err != nil {
			return 3 // UNKNOWN
		}
	}

	// Evaluate status
	status := 0 // OK

	if !invert {
		// Normal evaluation: higher values are worse
		if hasCritical && value >= critThreshold {
			status = 2 // CRITICAL
		} else if value >= warnThreshold {
			status = 1 // WARNING
		}
	} else {
		// Inverted evaluation: lower values are worse
		if hasCritical && value <= critThreshold {
			status = 2 // CRITICAL
		} else if value <= warnThreshold {
			status = 1 // WARNING
		}
	}

	return status
}

// getStatusText returns human-readable status text
func (m *MetricsProcessor) getStatusText(status int) string {
	switch status {
	case 0:
		return "OK"
	case 1:
		return "WARNING"
	case 2:
		return "CRITICAL"
	case 3:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// buildPerfData builds Nagios performance data string
func (m *MetricsProcessor) buildPerfData(name string, value float64, warning, critical, unit string) string {
	// Clean label name (no spaces, special chars)
	cleanName := m.cleanPerfDataLabel(name)

	// Convert unit to standard Nagios UOM
	standardUOM := m.convertToStandardUOM(unit)

	// Determine min/max values based on metric type
	min, max := m.getPerfDataMinMax(unit, value)

	// Format: label=value[UOM];[warn];[crit];[min];[max]
	perfData := fmt.Sprintf("%s=%.2f%s;%s;%s;%s;%s",
		cleanName, value, standardUOM, warning, critical, min, max)

	return perfData
}

// cleanPerfDataLabel removes invalid characters from performance data labels
func (m *MetricsProcessor) cleanPerfDataLabel(name string) string {
	// Replace spaces and dots with underscores
	cleaned := strings.ReplaceAll(name, " ", "_")
	cleaned = strings.ReplaceAll(cleaned, ".", "_")
	cleaned = strings.ReplaceAll(cleaned, "-", "_")

	// Remove special characters except underscore
	var result strings.Builder
	for _, r := range cleaned {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// convertToStandardUOM converts units to standard Nagios units of measurement
func (m *MetricsProcessor) convertToStandardUOM(unit string) string {
	switch strings.ToLower(unit) {
	case "bytes":
		return "B"
	case "kilobytes", "kb":
		return "KB"
	case "megabytes", "mb":
		return "MB"
	case "gigabytes", "gb":
		return "GB"
	case "milliseconds", "ms":
		return "ms"
	case "seconds", "s":
		return "s"
	case "percent", "%":
		return "%"
	case "celsius", "°c":
		return "°C"
	case "fahrenheit", "°f":
		return "°F"
	default:
		return unit
	}
}

// getPerfDataMinMax determines appropriate min/max values for performance data
func (m *MetricsProcessor) getPerfDataMinMax(unit string, value float64) (string, string) {
	switch strings.ToLower(unit) {
	case "%", "percent":
		return "0", "100"
	case "°c", "celsius":
		return "0", "100"
	case "ms", "milliseconds", "s", "seconds":
		return "0", ""
	case "bytes", "b", "kb", "mb", "gb":
		return "0", ""
	case "#", "count", "counter":
		return "0", ""
	default:
		return "", ""
	}
}

// GenerateSimpleNagiosResponse generates a simple Nagios response for probe-based queries
func (m *MetricsProcessor) GenerateSimpleNagiosResponse(probeName string, metrics []CachedMetric) NagiosResponse {
	if len(metrics) == 0 {
		return NagiosResponse{
			Status:     2, // CRITICAL
			StatusText: "CRITICAL",
			Message:    fmt.Sprintf("No metrics available for probe %s", probeName),
			PerfData:   "",
		}
	}

	// Simple health check - just report metrics count and build basic perf data
	var perfDataItems []string
	metricCount := 0

	// Track reachability via the canonical OTel availability gauge: every
	// probe's up/reachability metric is named "<...>.up" (e.g.
	// senhub.clickhouse.up, modbus.up). The target is considered
	// unreachable only when ALL up-metrics report 0, so a single down
	// component gauge (ceph.osd.up, envoy.cluster.up, ...) does not flag
	// the whole probe CRITICAL.
	upMetricsSeen := 0
	upMetricsZero := 0

	for _, metric := range metrics {
		// Transform metric name using the same logic as PRTG
		transformedName, _ := m.TransformMetricNameForPRTG("", metric)
		cleanName := m.cleanPerfDataLabel(transformedName)

		// Convert metric value to float64 (handle different types)
		if val, ok := m.convertToFloat64(metric.Value); ok {
			perfDataItems = append(perfDataItems, fmt.Sprintf("%s=%.2f", cleanName, val))
			metricCount++

			if strings.HasSuffix(metric.MetricName, ".up") {
				upMetricsSeen++
				if val == 0 {
					upMetricsZero++
				}
			}
		}
	}

	perfData := strings.Join(perfDataItems, " ")

	// Target unreachable: an up-metric exists and every up-signal is 0.
	if upMetricsSeen > 0 && upMetricsZero == upMetricsSeen {
		return NagiosResponse{
			Status:     2, // CRITICAL
			StatusText: "CRITICAL",
			Message:    fmt.Sprintf("Probe %s target unreachable (up=0)", probeName),
			PerfData:   perfData,
		}
	}

	return NagiosResponse{
		Status:     0, // OK
		StatusText: "OK",
		Message:    fmt.Sprintf("Probe %s healthy - %d metrics collected", probeName, metricCount),
		PerfData:   perfData,
	}
}

// convertToFloat64 converts various types to float64 for Nagios processing
func (m *MetricsProcessor) convertToFloat64(value interface{}) (float64, bool) {
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
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed, true
		}
		return 0, false
	default:
		// Try to convert via string representation
		if str := fmt.Sprintf("%v", v); str != "" {
			if parsed, err := strconv.ParseFloat(str, 64); err == nil {
				return parsed, true
			}
		}
		return 0, false
	}
}

// Generate API Examples

// GenerateExamples creates example API calls for the probe
func (m *MetricsProcessor) GenerateExamples(probeName string, tags map[string]TagInfo, metrics []string) []MetricExample {
	var examples []MetricExample
	baseURL := fmt.Sprintf("/api/{agentkey}/prtg/metrics/%s", probeName)

	// Example 1: Basic usage
	examples = append(examples, MetricExample{
		Description: "Get all metrics for this probe",
		URL:         baseURL,
		ResultCount: len(metrics),
	})

	// Example 2: Limit results
	if len(metrics) > 5 {
		examples = append(examples, MetricExample{
			Description: "Get first 5 metrics",
			URL:         baseURL + "?limit=5",
			ResultCount: 5,
		})
	}

	// Example 3: Tag filtering (if tags available)
	if len(tags) > 0 {
		for tagName, tagInfo := range tags {
			if len(tagInfo.Values) > 1 {
				firstValue := tagInfo.Values[0]
				examples = append(examples, MetricExample{
					Description: fmt.Sprintf("Filter by %s = %s", tagName, firstValue),
					URL:         fmt.Sprintf("%s?tags=%s:%s", baseURL, tagName, firstValue),
					ResultCount: len(metrics) / len(tagInfo.Values), // rough estimate
				})
				break
			}
		}
	}

	// Example 4: Specific metrics
	if len(metrics) > 2 {
		firstMetric := metrics[0]
		examples = append(examples, MetricExample{
			Description: fmt.Sprintf("Get specific metric: %s", firstMetric),
			URL:         fmt.Sprintf("%s?metrics=%s", baseURL, firstMetric),
			ResultCount: 1,
		})
	}

	return examples
}

// Health Metrics Processing for Nagios

// isHealthStatusMetricNagios determines if a metric should use health-specific Nagios processing
func (m *MetricsProcessor) isHealthStatusMetricNagios(metricName string, metrics []CachedMetric) bool {
	// Check metric name patterns
	healthKeywords := []string{"health", "status", "state", "power_state", "availability"}

	metricLower := strings.ToLower(metricName)
	for _, keyword := range healthKeywords {
		if strings.Contains(metricLower, keyword) {
			return true
		}
	}

	// Check metric tags if available
	if len(metrics) > 0 {
		metric := metrics[0] // Use first metric to check patterns

		// Check if metric unit suggests health values
		if metric.Unit == "#" || metric.Unit == "" {
			// Numeric health values (0=OK, 1=Warning, 2=Critical, 3=Unknown)
			if val, ok := metric.Value.(float64); ok {
				if val >= 0 && val <= 3 && val == float64(int(val)) {
					return true
				}
			}
		}
	}

	return false
}

// processNagiosHealthMetric processes health/status metrics with special Nagios mapping
func (m *MetricsProcessor) processNagiosHealthMetric(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	m.logger.Debug().
		Str("channel", metricDef.Channel).
		Int("metric_count", len(metrics)).
		Msg("Processing health metric for Nagios")

	// Process health metrics individually (no aggregation makes sense for health)
	var worstStatus int = 0 // Start with OK
	var healthMessages []string
	var perfDataItems []string

	for _, metric := range metrics {
		// Convert health value to Nagios status
		healthValue, nagiosStatus, statusText := m.convertHealthToNagiosStatus(metric)

		// Track worst status
		if nagiosStatus > worstStatus {
			worstStatus = nagiosStatus
		}

		// Build message with context
		var contextName string
		if componentName, exists := metric.Tags["component"]; exists {
			contextName = componentName
		} else if instanceName, exists := metric.Tags["instance"]; exists {
			contextName = instanceName
		} else {
			contextName = metricDef.Channel
		}

		healthMessage := fmt.Sprintf("%s: %s", contextName, statusText)
		healthMessages = append(healthMessages, healthMessage)

		// Add performance data optimized for health metrics (no warning/critical thresholds, use min/max range)
		perfData := m.buildHealthPerfData(contextName, healthValue)
		perfDataItems = append(perfDataItems, perfData)
	}

	// Build overall message
	overallStatusText := m.getStatusText(worstStatus)
	var message string

	if len(healthMessages) == 1 {
		message = healthMessages[0]
	} else {
		// Multiple health metrics - show summary
		okCount := 0
		warnCount := 0
		critCount := 0
		unknownCount := 0

		for _, metric := range metrics {
			_, status, _ := m.convertHealthToNagiosStatus(metric)
			switch status {
			case 0:
				okCount++
			case 1:
				warnCount++
			case 2:
				critCount++
			case 3:
				unknownCount++
			}
		}

		message = fmt.Sprintf("%s - %s: %d OK, %d Warning, %d Critical, %d Unknown",
			overallStatusText, metricDef.Channel, okCount, warnCount, critCount, unknownCount)

		// Add specific failures for non-OK statuses
		if worstStatus > 0 {
			var failureMessages []string
			for _, metric := range metrics {
				_, status, statusText := m.convertHealthToNagiosStatus(metric)
				if status > 0 {
					var contextName string
					if componentName, exists := metric.Tags["component"]; exists {
						contextName = componentName
					} else {
						contextName = "component"
					}
					failureMessages = append(failureMessages, fmt.Sprintf("%s: %s", contextName, statusText))
				}
			}
			if len(failureMessages) > 0 {
				message += "; " + strings.Join(failureMessages, ", ")
			}
		}
	}

	return NagiosMetricResult{
		Status:   worstStatus,
		Message:  message,
		PerfData: strings.Join(perfDataItems, " "),
	}
}

// convertHealthToNagiosStatus converts health metric values to Nagios status codes
func (m *MetricsProcessor) convertHealthToNagiosStatus(metric CachedMetric) (float64, int, string) {
	// Convert metric value to float64
	var healthValue float64
	switch v := metric.Value.(type) {
	case float64:
		healthValue = v
	case float32:
		healthValue = float64(v)
	case int:
		healthValue = float64(v)
	case int32:
		healthValue = float64(v)
	case int64:
		healthValue = float64(v)
	default:
		return 0, 3, "UNKNOWN - Invalid health value type"
	}

	// Try to use lookup if available
	if m.lookupRegistry != nil {
		// Get probe type from tags to load transformer
		if probeType, exists := metric.Tags["probe_type"]; exists && probeType != "" {
			// Get transformer for this metric to find lookup reference
			transformer, err := m.formatConverter.transformerRegistry.LoadTransformer(probeType, "friendly")
			if err == nil && transformer != nil {
				lookupID := transformer.GetLookup(metric.MetricName)
				if lookupID != "" {
					// Get lookup definition
					if lookup, found := m.lookupRegistry.GetLookup(lookupID); found {
						// Convert healthValue to int for lookup
						intValue := int(healthValue)
						if lookupValue, exists := lookup.Mappings[intValue]; exists {
							// Map severity to Nagios status
							nagiosStatus := m.severityToNagiosStatus(lookupValue.Severity)
							statusText := lookupValue.Text

							// Add description if available and status is not OK
							if lookupValue.Description != "" && nagiosStatus > 0 {
								statusText = fmt.Sprintf("%s (%s)", lookupValue.Text, lookupValue.Description)
							}

							return healthValue, nagiosStatus, statusText
						}
					}
				}
			}
		}
	}

	// Fallback: Standard Redfish/generic health mapping
	// 0 = OK/Healthy, 1 = Warning/Degraded, 2 = Critical/Failed, 3 = Unknown
	switch int(healthValue) {
	case 0:
		return healthValue, 0, "OK" // Nagios OK
	case 1:
		return healthValue, 1, "WARNING" // Nagios WARNING
	case 2:
		return healthValue, 2, "CRITICAL" // Nagios CRITICAL
	case 3:
		return healthValue, 3, "UNKNOWN" // Nagios UNKNOWN
	default:
		// Handle boolean health metrics (0=unhealthy, 1=healthy)
		if healthValue == 1 {
			return healthValue, 0, "OK"
		} else if healthValue == 0 {
			return healthValue, 2, "CRITICAL"
		}

		// Unknown health value
		return healthValue, 3, fmt.Sprintf("UNKNOWN - Unexpected health value: %.0f", healthValue)
	}
}

// severityToNagiosStatus converts lookup severity to Nagios status code
func (m *MetricsProcessor) severityToNagiosStatus(severity string) int {
	switch strings.ToLower(severity) {
	case "ok":
		return 0 // Nagios OK
	case "warning":
		return 1 // Nagios WARNING
	case "error":
		return 2 // Nagios CRITICAL
	case "unknown":
		return 3 // Nagios UNKNOWN
	default:
		return 3 // Default to UNKNOWN for unrecognized severity
	}
}

// buildHealthPerfData builds optimized Nagios performance data for health metrics
func (m *MetricsProcessor) buildHealthPerfData(name string, value float64) string {
	// Clean label name (no spaces, special chars)
	cleanName := m.cleanPerfDataLabel(name)

	// Health metrics format: label=value;warning;critical;min;max
	// For health metrics with lookups, use desired_value as both warning and critical thresholds
	// This helps Nagios visualize the expected value

	// Try to get desired_value from lookup if available
	desiredValue := ""
	minValue := 0
	maxValue := 3 // Default range for generic health metrics

	// Note: We would need the metric name to look up the desired_value
	// For now, use generic format
	// Health metrics format: label=value;;;min;max
	// No warning/critical thresholds since health values have predefined meanings
	// min=0 (OK), max=3 (Unknown) to indicate the valid range

	if desiredValue != "" {
		// Include desired_value as warning/critical threshold when available
		return fmt.Sprintf("%s=%.0f;%s;%s;%d;%d", cleanName, value, desiredValue, desiredValue, minValue, maxValue)
	}

	return fmt.Sprintf("%s=%.0f;;;%d;%d", cleanName, value, minValue, maxValue)
}

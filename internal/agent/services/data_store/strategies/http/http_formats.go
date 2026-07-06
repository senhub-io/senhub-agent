// senhub-agent/internal/agent/services/data_store/http_formats.go
package http

import (
	"fmt"
	"strconv"
	"strings"
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
	// Extract probe type from tags (fallback to probe name if not present)
	// Transformers are registered by probe TYPE (redfish, cpu, citrix)
	// NOT by probe instance NAME (redfish, redfish2, cpu1, cpu2)
	probeType := metric.Tags["probe_type"]
	if probeType == "" {
		probeType = metric.ProbeName
	}

	// Get transformer for friendly name resolution using probe TYPE
	transformer, err := f.transformerRegistry.LoadTransformer(probeType, "friendly")
	if err != nil {
		f.logger.Warn().
			Err(err).
			Str("probe_name", metric.ProbeName).
			Str("probe_type", probeType).
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
		Name:      metric.MetricName,
		Channel:   channelName,
		Value:     metric.Value,
		Unit:      metric.Unit,
		Timestamp: metric.Timestamp,
		ProbeName: metric.ProbeName,
		Tags:      metric.Tags,
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

		// Extract probe type from tags (fallback to probe name if not present)
		probeType := metric.Tags["probe_type"]
		if probeType == "" {
			probeType = metric.ProbeName
		}

		// Generate unique key for transformer
		tsKey := f.cache.generateTimeSeriesKey(metric.ProbeName, probeType, metric.MetricName, metric.Tags)

		// Transform to PRTG channel
		if channel := f.transformToPRTGChannelWithFilter(tsKey, metric, filter); channel != nil {
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

// transformToPRTGChannelWithFilter converts a cached metric to PRTG channel format with filter options
func (f *FormatConverter) transformToPRTGChannelWithFilter(key string, metric CachedMetric, filter MetricFilter) *PRTGChannel {
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
	channelName, unit, valueLookup := f.transformMetricNameForPRTGWithLookup(key, metric)

	// Post-process channel name: remove discriminant tags if ShowTags=false
	if !filter.ShowTags {
		probeType := metric.Tags["probe_type"]
		if probeType == "" {
			probeType = metric.ProbeName
		}
		channelName = f.removeDiscriminantTagsFromChannelName(channelName, metric, probeType)
	}

	// Create PRTG channel
	channel := &PRTGChannel{
		Channel: channelName,
		Value:   value,
	}

	// Configure channel based on whether it uses lookup or not
	if valueLookup != "" {
		// Lookup metrics: PRTG will display text from lookup file
		// CRITICAL: Do not set Float field at all for lookup metrics (leave as nil)
		channel.ValueLookup = valueLookup
		// No Unit or CustomUnit for lookup metrics
	} else {
		// Regular metrics: use standard numeric formatting
		floatValue := 1
		channel.Float = &floatValue // PRTG expects Float=1 for decimal values

		// Derive the PRTG unit from the OTel mapping when available
		// (rate vs absolute, byte context), falling back to the
		// legacy display-unit switch.
		f.applyPRTGUnit(channel, unit, metric)
	}

	return channel
}

// applyPRTGUnit sets Unit/CustomUnit/SpeedSize/SpeedTime on a PRTG
// channel. The OTel mapping is the semantic source of truth: a metric
// whose otel.unit ends in "/s" is a rate and must render as a speed,
// never as an absolute volume/count; byte metrics take a
// context-appropriate PRTG byte unit instead of a blanket BytesMemory.
func (f *FormatConverter) applyPRTGUnit(channel *PRTGChannel, displayUnit string, metric CachedMetric) {
	otel := f.otelMappingFor(metric)

	if otel != nil && strings.HasSuffix(otel.Unit, "/s") {
		f.applyPRTGRateUnit(channel, otel.Unit, metric)
		return
	}

	if displayUnit == "" {
		return
	}

	switch displayUnit {
	case "#":
		channel.Unit = "Count"
	case "%":
		channel.Unit = "Percent"
	case "Bytes", "bytes", "By", "B":
		channel.Unit = f.prtgByteContext(otel, metric)
	case "°C":
		channel.Unit = "Temperature"
	case "ms":
		channel.Unit = "TimeResponse"
	case "s":
		channel.Unit = "TimeSeconds"
	default:
		channel.Unit = "Custom"
		channel.CustomUnit = displayUnit
	}
}

// applyPRTGRateUnit renders a per-second metric as a PRTG speed.
// Byte/bit rates use the native Speed* units with the input scale
// declared (the raw value stays per-second in the declared size);
// other rates use a Custom unit carrying the "/s" suffix.
func (f *FormatConverter) applyPRTGRateUnit(channel *PRTGChannel, otelUnit string, metric CachedMetric) {
	switch strings.ToLower(otelUnit) {
	case "by/s", "byte/s", "bytes/s":
		if f.isDiskContext(f.otelMappingFor(metric), metric) {
			channel.Unit = "SpeedDisk"
		} else {
			channel.Unit = "SpeedNet"
		}
		channel.SpeedSize = "Byte"
		channel.SpeedTime = "Second"
	case "bit/s", "bits/s":
		channel.Unit = "SpeedNet"
		channel.SpeedSize = "Bit"
		channel.SpeedTime = "Second"
	case "mbit/s", "mbits/s":
		channel.Unit = "SpeedNet"
		channel.SpeedSize = "MegaBit"
		channel.SpeedTime = "Second"
	default:
		channel.Unit = "Custom"
		channel.CustomUnit = humanizeRateUnit(otelUnit)
	}
}

// humanizeRateUnit turns a UCUM-ish rate unit into a display suffix:
// "{packet}/s" → "pkt/s", "{error}/s" → "err/s", "1/s" → "/s".
func humanizeRateUnit(otelUnit string) string {
	base := strings.TrimSuffix(otelUnit, "/s")
	base = strings.TrimPrefix(base, "{")
	base = strings.TrimSuffix(base, "}")
	switch base {
	case "1", "":
		return "/s"
	case "packet":
		return "pkt/s"
	case "error":
		return "err/s"
	case "operation":
		return "ops/s"
	case "request":
		return "req/s"
	default:
		return base + "/s"
	}
}

// prtgByteContext picks the PRTG byte unit matching what the bytes
// measure: memory, disk/storage, network bandwidth, or files.
// Memory is checked before the generic "file" heuristic so that
// metrics like container.memory.active_file stay in BytesMemory.
func (f *FormatConverter) prtgByteContext(otel *transformers.OtelMapping, metric CachedMetric) string {
	if f.isDiskContext(otel, metric) {
		return "BytesDisk"
	}
	if f.isNetworkContext(otel, metric) {
		return "BytesBandwidth"
	}
	if otelNameContains(otel, "memory", "swap") {
		return "BytesMemory"
	}
	if otelNameContains(otel, "file") {
		return "BytesFile"
	}
	return "BytesMemory"
}

func (f *FormatConverter) isDiskContext(otel *transformers.OtelMapping, metric CachedMetric) bool {
	if otelNameContains(otel, "disk", "filesystem", "storage", "volume", "asp") {
		return true
	}
	switch probeTypeOf(metric) {
	case "logicaldisk", "veeam":
		return true
	}
	return false
}

func (f *FormatConverter) isNetworkContext(otel *transformers.OtelMapping, metric CachedMetric) bool {
	if otelNameContains(otel, "network", "interface", "bandwidth", "traffic") {
		return true
	}
	switch probeTypeOf(metric) {
	case "network", "snmp_poll", "netscaler", "wifi_signal_strength":
		return true
	}
	return false
}

func otelNameContains(otel *transformers.OtelMapping, keywords ...string) bool {
	if otel == nil || otel.Name == "" {
		return false
	}
	name := strings.ToLower(otel.Name)
	for _, kw := range keywords {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

func probeTypeOf(metric CachedMetric) string {
	if pt := metric.Tags["probe_type"]; pt != "" {
		return pt
	}
	return metric.ProbeName
}

// otelMappingFor resolves the OTel mapping declared for a metric in
// its probe's transformer definition, or nil when unavailable (legacy
// transformer, unknown probe, unmapped metric). LoadTransformer caches
// per probe, so repeated resolution is a map hit.
func (f *FormatConverter) otelMappingFor(metric CachedMetric) *transformers.OtelMapping {
	transformer, err := f.transformerRegistry.LoadTransformer(probeTypeOf(metric), "friendly")
	if err != nil || transformer == nil {
		return nil
	}
	otelAware, ok := transformer.(transformers.OtelAware)
	if !ok {
		return nil
	}
	return otelAware.GetOtelMapping(metric.MetricName)
}

// transformToPRTGChannel converts a cached metric to PRTG channel format (legacy - no filter)
func (f *FormatConverter) transformToPRTGChannel(key string, metric CachedMetric) *PRTGChannel {
	// Call new function with empty filter (ShowTags defaults to true)
	return f.transformToPRTGChannelWithFilter(key, metric, MetricFilter{ShowTags: true})
}

// removeDiscriminantTagsFromChannelName removes discriminant tag prefixes/suffixes from channel names
// Example: "Interface LO/1 - State" -> "State" when filtering on interface:LO/1
// Example: "SSL Certificate Status (Wildcard_ACME-CORP_2025)" -> "SSL Certificate Status"
func (f *FormatConverter) removeDiscriminantTagsFromChannelName(channelName string, metric CachedMetric, probeType string) string {
	// Get discriminant tags for this probe type
	discriminantTags, exists := DiscriminantTagsRegistry[probeType]
	if !exists {
		return channelName // No discriminant tags defined, return as-is
	}

	// Try to remove each discriminant tag value from the channel name
	for _, tagKey := range discriminantTags {
		if tagValue, exists := metric.Tags[tagKey]; exists && tagValue != "" {
			// Try patterns at the beginning: "TagValue - MetricName"
			prefixPatterns := []string{
				tagValue + " - ",
				tagValue + ": ",
				tagValue + " ",
			}

			for _, pattern := range prefixPatterns {
				if strings.HasPrefix(channelName, pattern) {
					// Remove the prefix
					channelName = strings.TrimPrefix(channelName, pattern)
					f.logger.Debug().
						Str("original", pattern+channelName).
						Str("cleaned", channelName).
						Str("tag_key", tagKey).
						Str("tag_value", tagValue).
						Msg("Removed discriminant tag prefix from channel name")
					break
				}
			}

			// Try patterns at the end: "MetricName (TagValue)"
			suffixPatterns := []string{
				" (" + tagValue + ")",
				" [" + tagValue + "]",
				" - " + tagValue,
			}

			for _, pattern := range suffixPatterns {
				if strings.HasSuffix(channelName, pattern) {
					// Remove the suffix
					channelName = strings.TrimSuffix(channelName, pattern)
					f.logger.Debug().
						Str("original", channelName+pattern).
						Str("cleaned", channelName).
						Str("tag_key", tagKey).
						Str("tag_value", tagValue).
						Msg("Removed discriminant tag suffix from channel name")
					break
				}
			}
		}
	}

	return channelName
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

// transformMetricNameForPRTG transforms metric names using the transformer system (legacy)
func (f *FormatConverter) transformMetricNameForPRTG(key string, metric CachedMetric) (string, string) {
	name, unit, _ := f.transformMetricNameForPRTGWithLookup(key, metric)
	return name, unit
}

// transformMetricNameForPRTGWithLookup transforms metric names using the transformer system with lookup support
func (f *FormatConverter) transformMetricNameForPRTGWithLookup(key string, metric CachedMetric) (string, string, string) {
	// Extract probe type from tags (fallback to probe name if not present)
	// Transformers are registered by probe TYPE (redfish, cpu, citrix)
	// NOT by probe instance NAME (redfish, redfish2, cpu1, cpu2)
	probeType := metric.Tags["probe_type"]
	if probeType == "" {
		probeType = metric.ProbeName
	}

	// Get transformer for friendly name resolution using probe TYPE
	transformer, err := f.transformerRegistry.LoadTransformer(probeType, "friendly")
	if err != nil {
		f.logger.Warn().
			Err(err).
			Str("probe_name", metric.ProbeName).
			Str("probe_type", probeType).
			Msg("Failed to get transformer for PRTG format")
		return metric.MetricName, metric.Unit, ""
	}

	// Default to original values
	transformedName := metric.MetricName
	unit := metric.Unit
	valueLookup := ""

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

		// Get lookup from transformer for health and status metrics
		if lookupName := transformer.GetLookup(metric.MetricName); lookupName != "" {
			// Check if this is a health/status metric that should use lookups
			if f.isHealthStatusMetric(metric.MetricName, metric.Tags) {
				valueLookup = lookupName
				f.logger.Debug().
					Str("metric", metric.MetricName).
					Str("lookup", lookupName).
					Msg("Applied lookup for health/status metric")
			} else {
				f.logger.Debug().
					Str("metric", metric.MetricName).
					Str("lookup", lookupName).
					Msg("Lookup found but metric not identified as health/status metric")
			}
		} else {
			// Check if this might be a health metric that should have a lookup
			if f.isHealthStatusMetric(metric.MetricName, metric.Tags) {
				f.logger.Debug().
					Str("metric", metric.MetricName).
					Msg("Health/status metric detected but no lookup found in transformer")
			}
		}
	}

	return transformedName, unit, valueLookup
}

// isHealthStatusMetric determines if a metric should use value lookups
func (f *FormatConverter) isHealthStatusMetric(metricName string, tags map[string]string) bool {
	// Check metric name patterns for health/status indicators
	healthKeywords := []string{"health", "status", "state", "power_state", "availability", "bottleneck"}

	metricLower := strings.ToLower(metricName)
	for _, keyword := range healthKeywords {
		if strings.Contains(metricLower, keyword) {
			return true
		}
	}

	return false
}

// Metric Filtering

// applyMetricFilter applies filtering criteria to metrics with contextual intelligence
func (f *FormatConverter) applyMetricFilter(metrics []CachedMetric, filter MetricFilter) []CachedMetric {
	if filter.Limit == 0 && filter.Offset == 0 && len(filter.MetricNames) == 0 && len(filter.TagFilters) == 0 && len(filter.ExcludeTags) == 0 {
		return metrics // No filtering needed
	}

	// Apply contextual filtering based on tag types
	contextualMetricPrefixes := f.getContextualMetricPrefixes(filter.TagFilters)

	filtered := make([]CachedMetric, 0)

	for _, metric := range metrics {
		// Apply contextual metric filtering first
		if len(contextualMetricPrefixes) > 0 {
			matchesContext := false
			for _, prefix := range contextualMetricPrefixes {
				if strings.HasPrefix(metric.MetricName, prefix) {
					matchesContext = true
					break
				}
			}
			if !matchesContext {
				f.logger.Debug().
					Str("metric", metric.MetricName).
					Strs("context_prefixes", contextualMetricPrefixes).
					Msg("Metric filtered out by contextual filtering")
				continue
			}
		}

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

		// Filter by tag filters if specified (include tags): the metric must
		// carry every filtered tag with one of the allowed values.
		if len(filter.TagFilters) > 0 {
			tagMatch := true
			for tagKey, allowedValues := range filter.TagFilters {
				metricValue, exists := metric.Tags[tagKey]
				if !exists {
					tagMatch = false
					break
				}
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

// getContextualMetricPrefixes determines which metric prefixes to include based on tag filter context
func (f *FormatConverter) getContextualMetricPrefixes(tagFilters map[string][]string) []string {
	var prefixes []string

	// Check for storage-related contextual tags
	for tagKey := range tagFilters {
		switch tagKey {
		case "pool_name":
			// If filtering on pool_name, show only pool metrics
			// Support both old test format and real hardware format
			prefixes = append(prefixes, "storage.pool.", "hardware.storage.pool.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for pool metrics")

		case "volume_name":
			// If filtering on volume_name, show only volume metrics
			prefixes = append(prefixes, "storage.volume.", "hardware.storage.volume.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for volume metrics")

		case "drive_name":
			// If filtering on drive_name, show only drive metrics
			prefixes = append(prefixes, "storage.drive.", "hardware.storage.drive.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for drive metrics")

		case "controller":
			// If filtering on controller, show only storage controller metrics
			prefixes = append(prefixes, "storage.controller.", "hardware.storage.controller.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for controller metrics")

		// case "fan_name":
		//     // Thermal metrics disabled - fan filtering not available
		//     f.logger.Debug().Str("tag", tagKey).Msg("Fan metrics disabled for consistency")

		case "psu_name":
			// If filtering on psu_name, show only power/PSU metrics
			prefixes = append(prefixes, "hardware.power.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for PSU metrics")

		case "interface", "adapter", "connection_name":
			// If filtering on interface/adapter/connection, show only network metrics
			// Support both dot notation (hardware probes) and underscore notation (host probes)
			// ALSO support netscaler interface metrics
			prefixes = append(prefixes, "network.", "network_", "packets_", "bytes_", "errors_", "discards_", "hardware.network.", "netscaler.interface.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for network metrics")

		case "core":
			// If filtering on core, show only CPU metrics
			// Support both dot notation (hardware probes) and underscore notation (host probes)
			prefixes = append(prefixes, "cpu.", "cpu_", "hardware.cpu.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for CPU metrics")

		case "mount_point", "filesystem", "drive":
			// If filtering on mount point/filesystem/drive, show only logical disk metrics
			// Support both dot notation (hardware probes) and underscore notation (host probes)
			prefixes = append(prefixes, "logicaldisk.", "disk_", "hardware.disk.")
			f.logger.Debug().Str("tag", tagKey).Msg("Applied contextual filtering for logical disk metrics")
		}
	}

	// If multiple contextual prefixes found, return them all (user might want to see related metrics)
	return prefixes
}

// Nagios Format Conversion (basic structure - detailed implementation would be in Nagios module)

// GetNagiosMetricsForProbe converts cached metrics to basic Nagios format
func (f *FormatConverter) GetNagiosMetricsForProbe(probeName string) NagiosResponse {
	f.logger.Debug().Str("probe", probeName).Msg("Converting metrics to basic Nagios format")

	// Get cached metrics for the probe
	cachedMetrics := f.cache.GetProbeMetrics(probeName)

	// Simple conversion - for complex Nagios logic, this would delegate to a specialized module
	status := 0 // 0=OK
	statusText := "OK"
	message := fmt.Sprintf("Probe %s has %d metrics", probeName, len(cachedMetrics))

	if len(cachedMetrics) == 0 {
		status = 1 // 1=WARNING
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

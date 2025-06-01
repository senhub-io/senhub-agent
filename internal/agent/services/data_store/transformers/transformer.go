// senhub-agent/internal/agent/services/data_store/transformers/transformer.go
package transformers

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/services/logger"
)

//go:embed definitions/*.yaml definitions/shared/*.yaml lookups/*.lookup
var definitionFiles embed.FS

// MetricTransformer defines the interface for transforming metric names
type MetricTransformer interface {
	TransformMetricName(key string, tags map[string]string) string
	GetUnit(key string) string
	GetLookup(key string) string
}

// TransformConfig represents the structure of a transformation YAML file (legacy)
type TransformConfig struct {
	Patterns map[string]string `yaml:"patterns"`
	Units    map[string]string `yaml:"units"`
}

// MetricDefinition represents a single metric definition in the new YAML format
type MetricDefinition struct {
	Name                  string            `yaml:"name"`
	Channel               string            `yaml:"channel"`
	DisplayName           string            `yaml:"display_name"`
	Unit                  string            `yaml:"unit"`
	MultiInstanceLabels   []string          `yaml:"multi_instance_labels"`
	TagFilter             map[string]string `yaml:"tag_filter"`
	Description           string            `yaml:"description"`
	CalculatedMetric      string            `yaml:"calculated_metric"`
	AlertThresholdWarning int               `yaml:"alert_threshold_warning"`
	AlertThresholdCritical int              `yaml:"alert_threshold_critical"`
	Lookup                string            `yaml:"lookup"`
}

// ProbeDefinition represents the structure of a probe definition YAML file
type ProbeDefinition struct {
	ProbeName string             `yaml:"probe_name"`
	Version   string             `yaml:"version"`
	Metrics   []MetricDefinition `yaml:"metrics"`
}

// UnitDefinition represents a unit mapping definition
type UnitDefinition struct {
	Aliases     []string `yaml:"aliases"`
	Standard    string   `yaml:"standard"`
	Description string   `yaml:"description"`
}

// UnitsConfig represents the units.yaml structure
type UnitsConfig struct {
	Units map[string]UnitDefinition `yaml:"units"`
}

// TemplateConfig represents the templates.yaml structure
type TemplateConfig struct {
	Templates       map[string]string            `yaml:"templates"`
	MetricTypes     map[string]string            `yaml:"metric_types"`
	TagPriority     map[int][]string             `yaml:"tag_priority"`
	FallbackPatterns map[string]string           `yaml:"fallback_patterns"`
}

// ProbeTransformer implements MetricTransformer for a specific probe and style (legacy)
type ProbeTransformer struct {
	probeName string
	style     string
	config    TransformConfig
	logger    *logger.Logger
}

// DefinitionBasedTransformer implements MetricTransformer using YAML definitions
type DefinitionBasedTransformer struct {
	probeName    string
	definition   *ProbeDefinition
	unitsConfig  *UnitsConfig
	templatesConfig *TemplateConfig
	logger       *logger.Logger
}

// TransformerRegistry manages all transformers
type TransformerRegistry struct {
	transformers map[string]MetricTransformer // key: "probe_name:style"
	logger       *logger.Logger
}

// NewTransformerRegistry creates a new transformer registry
func NewTransformerRegistry(logger *logger.Logger) *TransformerRegistry {
	localLogger := logger.With().Str("component", "TransformerRegistry").Logger()
	return &TransformerRegistry{
		transformers: make(map[string]MetricTransformer),
		logger:       &localLogger,
	}
}

// LoadTransformer loads or creates a transformer for a specific probe and style
func (tr *TransformerRegistry) LoadTransformer(probeName, style string) (MetricTransformer, error) {
	key := fmt.Sprintf("%s:%s", probeName, style)
	
	// Return cached transformer if already loaded
	if transformer, exists := tr.transformers[key]; exists {
		return transformer, nil
	}

	// Try to load new definition-based transformer first
	transformer, err := tr.loadDefinitionBasedTransformer(probeName)
	if err == nil {
		// Cache the transformer
		tr.transformers[key] = transformer
		tr.logger.Debug().
			Str("probe", probeName).
			Str("style", style).
			Msg("✅ Definition-based transformer loaded successfully")
		return transformer, nil
	}

	// Log the error and create fallback transformer directly
	tr.logger.Warn().
		Err(err).
		Str("probe", probeName).
		Msg("❌ Definition-based transformer not found, creating fallback")
		
	// Create fallback transformer directly instead of loading from file
	transformer = tr.createFallbackTransformer(probeName, style)
	
	// Cache the transformer
	tr.transformers[key] = transformer
	tr.logger.Debug().
		Str("probe", probeName).
		Str("style", style).
		Msg("🔧 Fallback transformer created")

	return transformer, nil
}

// REMOVED: loadTransformerFromFile - replaced by embedded definitions

// createFallbackTransformer creates a basic transformer when no config file exists
func (tr *TransformerRegistry) createFallbackTransformer(probeName, style string) MetricTransformer {
	return &ProbeTransformer{
		probeName: probeName,
		style:     style,
		config: TransformConfig{
			Patterns: make(map[string]string),
			Units:    make(map[string]string),
		},
		logger: tr.logger,
	}
}

// TransformMetricName transforms a metric key to a user-friendly name
func (pt *ProbeTransformer) TransformMetricName(key string, tags map[string]string) string {
	// Look for exact pattern match first
	if pattern, exists := pt.config.Patterns[key]; exists {
		return pt.applyTemplate(pattern, tags)
	}

	// Look for partial pattern matches
	for pattern, template := range pt.config.Patterns {
		if pt.matchesPattern(key, pattern) {
			return pt.applyTemplate(template, tags)
		}
	}

	// Fallback: use key as-is but make it more readable
	return pt.makeReadable(key)
}

// GetUnit returns the unit for a metric key
func (pt *ProbeTransformer) GetUnit(key string) string {
	// Direct lookup
	if unit, exists := pt.config.Units[key]; exists {
		return unit
	}

	// Pattern-based lookup
	for pattern, unit := range pt.config.Units {
		if pt.matchesPattern(key, pattern) {
			return unit
		}
	}

	return ""
}

// GetLookup returns the lookup file for a metric key (legacy transformer doesn't support lookups)
func (pt *ProbeTransformer) GetLookup(key string) string {
	// Legacy transformers don't support lookups
	return ""
}

// matchesPattern checks if a key matches a pattern with wildcards
func (pt *ProbeTransformer) matchesPattern(key, pattern string) bool {
	// Simple pattern matching for now - could be enhanced with regex
	if strings.Contains(pattern, "{") {
		// Remove wildcard parts for basic matching
		patternBase := strings.Split(pattern, "{")[0]
		return strings.HasPrefix(key, patternBase)
	}
	return key == pattern
}

// applyTemplate applies template variables to a pattern
func (pt *ProbeTransformer) applyTemplate(template string, tags map[string]string) string {
	result := template

	// Replace {index} with numeric index from key or tags
	if strings.Contains(result, "{index}") {
		index := pt.extractIndex(tags)
		result = strings.ReplaceAll(result, "{index}", index)
	}

	// Replace {component} with component name from tags
	if strings.Contains(result, "{component}") {
		component := tags["component"]
		if component == "" {
			component = "Unknown"
		}
		result = strings.ReplaceAll(result, "{component}", component)
	}

	return result
}

// extractIndex extracts an index number from tags or key
func (pt *ProbeTransformer) extractIndex(tags map[string]string) string {
	// Try to get index from tags first
	if index, exists := tags["index"]; exists {
		return index
	}
	
	// Try to get from instance tag
	if instance, exists := tags["instance"]; exists {
		return instance
	}

	return "0"
}

// makeReadable converts technical keys to more readable format
func (pt *ProbeTransformer) makeReadable(key string) string {
	// Remove probe-specific common prefixes that add no value
	readable := key
	
	// Redfish probe: remove "hardware." prefix (100% of metrics have it)
	if strings.HasPrefix(readable, "hardware.") {
		readable = strings.TrimPrefix(readable, "hardware.")
	}
	
	// Host probe prefixes that appear in most metrics of their type
	if strings.HasPrefix(readable, "disk_") {
		readable = strings.TrimPrefix(readable, "disk_")
	} else if strings.HasPrefix(readable, "memory_") {
		readable = strings.TrimPrefix(readable, "memory_")
	}
	
	// Basic transformation: replace dots and underscores with spaces, capitalize
	readable = strings.ReplaceAll(readable, ".", " ")
	readable = strings.ReplaceAll(readable, "_", " ")
	
	// Capitalize first letter of each word, with special cases
	words := strings.Fields(readable)
	for i, word := range words {
		if len(word) > 0 {
			// Special cases for technical terms
			if strings.ToLower(word) == "io" {
				words[i] = "IO"
			} else if strings.ToLower(word) == "iowait" {
				words[i] = "IO Wait"
			} else if strings.ToLower(word) == "cpu" {
				words[i] = "CPU"
			} else {
				words[i] = strings.ToUpper(word[:1]) + word[1:]
			}
		}
	}
	
	return strings.Join(words, " ")
}

// loadDefinitionBasedTransformer loads a new definition-based transformer
func (tr *TransformerRegistry) loadDefinitionBasedTransformer(probeName string) (MetricTransformer, error) {
	// Load probe definition from embedded files
	probeFilePath := fmt.Sprintf("definitions/%s.yaml", probeName)
	tr.logger.Debug().
		Str("probe", probeName).
		Str("file_path", probeFilePath).
		Msg("🔍 Loading probe definition from embedded files")
		
	definition, err := tr.loadProbeDefinitionFromEmbed(probeFilePath)
	if err != nil {
		tr.logger.Error().
			Err(err).
			Str("probe", probeName).
			Str("file_path", probeFilePath).
			Msg("❌ Failed to load embedded probe definition")
		return nil, fmt.Errorf("failed to load probe definition: %w", err)
	}
	
	tr.logger.Debug().
		Str("probe", probeName).
		Int("metrics_count", len(definition.Metrics)).
		Msg("✅ Probe definition loaded from embedded files")
	
	// Load shared configurations from embedded files
	unitsConfig, err := tr.loadUnitsConfigFromEmbed("definitions/shared/units.yaml")
	if err != nil {
		tr.logger.Warn().Err(err).Msg("Failed to load units config, using empty config")
		unitsConfig = &UnitsConfig{Units: make(map[string]UnitDefinition)}
	}
	
	templatesConfig, err := tr.loadTemplatesConfigFromEmbed("definitions/shared/templates.yaml")
	if err != nil {
		tr.logger.Warn().Err(err).Msg("Failed to load templates config, using empty config")
		templatesConfig = &TemplateConfig{
			Templates: make(map[string]string),
			MetricTypes: make(map[string]string),
			TagPriority: make(map[int][]string),
			FallbackPatterns: make(map[string]string),
		}
	}
	
	localLogger := tr.logger.With().Str("component", "DefinitionBasedTransformer").Str("probe", probeName).Logger()
	
	return &DefinitionBasedTransformer{
		probeName:       probeName,
		definition:      definition,
		unitsConfig:     unitsConfig,
		templatesConfig: templatesConfig,
		logger:          &localLogger,
	}, nil
}

// loadProbeDefinition loads a probe definition from YAML file
func (tr *TransformerRegistry) loadProbeDefinition(filePath string) (*ProbeDefinition, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var definition ProbeDefinition
	err = yaml.Unmarshal(data, &definition)
	if err != nil {
		return nil, err
	}
	
	return &definition, nil
}

// loadProbeDefinitionFromEmbed loads a probe definition from embedded YAML file
func (tr *TransformerRegistry) loadProbeDefinitionFromEmbed(filePath string) (*ProbeDefinition, error) {
	data, err := definitionFiles.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var definition ProbeDefinition
	err = yaml.Unmarshal(data, &definition)
	if err != nil {
		return nil, err
	}
	
	return &definition, nil
}

// loadUnitsConfig loads units configuration from YAML file
func (tr *TransformerRegistry) loadUnitsConfig(filePath string) (*UnitsConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var config UnitsConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	
	return &config, nil
}

// loadUnitsConfigFromEmbed loads units configuration from embedded YAML file
func (tr *TransformerRegistry) loadUnitsConfigFromEmbed(filePath string) (*UnitsConfig, error) {
	data, err := definitionFiles.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var config UnitsConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	
	return &config, nil
}

// loadTemplatesConfig loads templates configuration from YAML file
func (tr *TransformerRegistry) loadTemplatesConfig(filePath string) (*TemplateConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var config TemplateConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	
	return &config, nil
}

// loadTemplatesConfigFromEmbed loads templates configuration from embedded YAML file
func (tr *TransformerRegistry) loadTemplatesConfigFromEmbed(filePath string) (*TemplateConfig, error) {
	data, err := definitionFiles.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	
	var config TemplateConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	
	return &config, nil
}

// TransformMetricName implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) TransformMetricName(metricName string, tags map[string]string) string {
	// Find matching metric definition
	var matchingDef *MetricDefinition
	for _, metric := range dt.definition.Metrics {
		if dt.matchesMetric(metric, metricName, tags) {
			matchingDef = &metric
			break
		}
	}
	
	if matchingDef == nil {
		dt.logger.Debug().
			Str("metric_name", metricName).
			Interface("tags", tags).
			Msg("No matching definition found, using fallback")
		return dt.generateFallbackDisplayName(metricName, tags)
	}
	
	// Generate display name using definition
	displayName := dt.generateDisplayName(*matchingDef, tags)
	
	dt.logger.Debug().
		Str("metric_name", metricName).
		Str("display_name", displayName).
		Interface("tags", tags).
		Msg("Generated display name from definition")
	
	return displayName
}

// GetUnit implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) GetUnit(metricName string) string {
	// Find matching metric definition
	for _, metric := range dt.definition.Metrics {
		if metric.Name == metricName {
			return dt.normalizeUnit(metric.Unit)
		}
	}
	
	// Return empty string if no match found
	return ""
}

// GetLookup implements MetricTransformer interface for definition-based transformer
func (dt *DefinitionBasedTransformer) GetLookup(metricName string) string {
	// Find matching metric definition
	for _, metric := range dt.definition.Metrics {
		if metric.Name == metricName {
			return metric.Lookup
		}
	}
	
	// Return empty string if no match found
	return ""
}

// matchesMetric checks if a metric definition matches the given metric name and tags
func (dt *DefinitionBasedTransformer) matchesMetric(def MetricDefinition, metricName string, tags map[string]string) bool {
	// Check if metric name matches (with wildcard support)
	if !dt.matchesPattern(def.Name, metricName) {
		return false
	}
	
	// Check tag filters if defined
	if len(def.TagFilter) > 0 {
		for filterKey, filterValue := range def.TagFilter {
			tagValue, exists := tags[filterKey]
			if !exists {
				return false
			}
			
			// Handle special null value filter
			if filterValue == "null" || filterValue == "{webapp_url}" {
				// Custom logic for webapp filtering could be added here
				continue
			}
			
			if tagValue != filterValue {
				return false
			}
		}
	}
	
	return true
}

// matchesPattern checks if a pattern matches a string (supports {index} wildcards)
func (dt *DefinitionBasedTransformer) matchesPattern(pattern, text string) bool {
	// Simple exact match
	if pattern == text {
		return true
	}
	
	// Handle wildcards like {index}, {component}, etc.
	if strings.Contains(pattern, "{") {
		// Create a regex pattern by replacing template variables with appropriate regex
		regexPattern := pattern
		
		// Replace common template variables with regex patterns
		templateReplacements := map[string]string{
			"{index}":         `\d+`,
			"{component}":     `[^.]+`,
			"{pool_id}":       `[^.]+`,
			"{controller_id}": `[^.]+`,
			"{volume_id}":     `[^.]+`,
			"{drive_id}":      `[^.]+`,
			"{core}":          `\d+`,
			"{interface}":     `[^.]+`,
			"{device}":        `[^.]+`,
		}
		
		for placeholder, regex := range templateReplacements {
			regexPattern = strings.ReplaceAll(regexPattern, placeholder, regex)
		}
		
		// Escape dots for literal matching
		regexPattern = strings.ReplaceAll(regexPattern, ".", `\.`)
		
		// Add anchors for exact matching
		regexPattern = "^" + regexPattern + "$"
		
		// Use simple pattern matching for now (avoid regex import)
		// Check if the structure matches by comparing parts
		patternParts := strings.Split(pattern, ".")
		textParts := strings.Split(text, ".")
		
		if len(patternParts) != len(textParts) {
			return false
		}
		
		for i, patternPart := range patternParts {
			textPart := textParts[i]
			
			// If it's a template variable, accept any value
			if strings.Contains(patternPart, "{") && strings.Contains(patternPart, "}") {
				continue
			}
			
			// Otherwise, must match exactly
			if patternPart != textPart {
				return false
			}
		}
		
		return true
	}
	
	return false
}

// generateDisplayName creates a contextualized display name using the definition and tags
func (dt *DefinitionBasedTransformer) generateDisplayName(def MetricDefinition, tags map[string]string) string {
	displayName := def.DisplayName
	
	// If no display_name is defined, use the channel name
	if displayName == "" {
		displayName = def.Channel
	}
	
	// If still empty, generate from metric name
	if displayName == "" {
		displayName = dt.makeReadable(def.Name)
	}
	
	// Replace template variables with tag values using definition-specific labels
	displayName = dt.replaceTemplateVariablesWithDefinition(displayName, tags, def)
	
	return displayName
}

// replaceTemplateVariables replaces {variable} placeholders with tag values
func (dt *DefinitionBasedTransformer) replaceTemplateVariables(template string, tags map[string]string) string {
	result := template
	
	// Replace common template variables
	templateVars := []string{
		"index", "component", "core", "interface", "device", "mountpoint",
		"volume_name", "volume_id", "drive_name", "drive_id", "pool_id",
		"diskgroup_name", "diskgroup_id", "controller_name", "controller_id",
		"fan_name", "sensor_location", "target", "operation_type",
		"raid_type", "host", "system_name", "slot", "enclosure_id",
		"adapter_id", "memory_controller",
	}
	
	for _, varName := range templateVars {
		placeholder := fmt.Sprintf("{%s}", varName)
		if strings.Contains(result, placeholder) {
			value := tags[varName]
			
			// Handle special case mappings
			if value == "" {
				switch varName {
				case "core":
					// Try "index" tag if "core" tag is not found
					value = tags["index"]
				case "index":
					// Try "core" tag if "index" tag is not found
					value = tags["core"]
				case "device":
					// Try "interface" tag if "device" tag is not found
					value = tags["interface"]
				case "interface":
					// Try "device" tag if "interface" tag is not found
					value = tags["device"]
				case "drive_name":
					// Try "drive_id" tag if "drive_name" tag is not found
					value = tags["drive_id"]
				case "drive_id":
					// Try "drive_name" tag if "drive_id" tag is not found
					value = tags["drive_name"]
				}
			}
			
			if value == "" {
				// Fallback values
				switch varName {
				case "index", "core":
					value = "0"
				case "component":
					value = "Unknown"
				default:
					value = fmt.Sprintf("<%s>", varName)
				}
			}
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}
	
	return result
}

// replaceTemplateVariablesWithDefinition replaces {variable} placeholders using definition-specific multi_instance_labels and automatic detection
func (dt *DefinitionBasedTransformer) replaceTemplateVariablesWithDefinition(template string, tags map[string]string, def MetricDefinition) string {
	result := template
	
	dt.logger.Debug().
		Str("template", template).
		Interface("multi_instance_labels", def.MultiInstanceLabels).
		Interface("tags", tags).
		Msg("Processing template with definition-specific labels")
	
	// First, process multi_instance_labels defined for this specific metric
	for _, labelName := range def.MultiInstanceLabels {
		placeholder := fmt.Sprintf("{%s}", labelName)
		if strings.Contains(result, placeholder) {
			value := tags[labelName]
			
			if value != "" {
				result = strings.ReplaceAll(result, placeholder, value)
				dt.logger.Debug().
					Str("placeholder", placeholder).
					Str("value", value).
					Msg("Replaced template variable from multi_instance_labels")
			} else {
				dt.logger.Debug().
					Str("placeholder", placeholder).
					Str("label", labelName).
					Msg("Multi-instance label not found in tags")
			}
		}
	}
	
	// If we still have unresolved placeholders, try automatic index detection
	if strings.Contains(result, "{") {
		result = dt.replaceTemplateVariablesWithAutoDetection(result, tags)
	}
	
	// Then process any remaining placeholders with fallback logic
	result = dt.replaceTemplateVariables(result, tags)
	
	return result
}

// generateFallbackDisplayName creates a fallback display name when no definition matches
func (dt *DefinitionBasedTransformer) generateFallbackDisplayName(metricName string, tags map[string]string) string {
	// Try fallback patterns from templates config
	if len(dt.templatesConfig.FallbackPatterns) > 0 {
		// Use generic fallback pattern
		if pattern, exists := dt.templatesConfig.FallbackPatterns["generic"]; exists {
			return strings.ReplaceAll(pattern, "{metric_name}", dt.makeReadable(metricName))
		}
	}
	
	// Ultimate fallback: make the metric name readable
	return dt.makeReadable(metricName)
}

// makeReadable converts technical metric names to readable format
func (dt *DefinitionBasedTransformer) makeReadable(metricName string) string {
	// Remove probe-specific common prefixes that add no value
	readable := metricName
	
	// Redfish probe: remove "hardware." prefix (100% of metrics have it)
	if strings.HasPrefix(readable, "hardware.") {
		readable = strings.TrimPrefix(readable, "hardware.")
	}
	
	// Legacy prefixes for backwards compatibility
	legacyPrefixes := []string{"dell_powervault_me.", "thermal.", "power.", "storage."}
	for _, prefix := range legacyPrefixes {
		readable = strings.TrimPrefix(readable, prefix)
	}
	
	// Replace dots and underscores with spaces
	readable = strings.ReplaceAll(readable, ".", " ")
	readable = strings.ReplaceAll(readable, "_", " ")
	
	// Capitalize first letter of each word, with special cases
	words := strings.Fields(readable)
	for i, word := range words {
		if len(word) > 0 {
			// Special cases for technical terms
			if strings.ToLower(word) == "io" {
				words[i] = "IO"
			} else if strings.ToLower(word) == "iowait" {
				words[i] = "IO Wait"
			} else if strings.ToLower(word) == "cpu" {
				words[i] = "CPU"
			} else {
				words[i] = strings.ToUpper(word[:1]) + word[1:]
			}
		}
	}
	
	return strings.Join(words, " ")
}

// normalizeUnit converts unit aliases to standard units
func (dt *DefinitionBasedTransformer) normalizeUnit(unit string) string {
	// Check if unit needs normalization
	for _, unitDef := range dt.unitsConfig.Units {
		for _, alias := range unitDef.Aliases {
			if alias == unit {
				return unitDef.Standard
			}
		}
	}
	
	// Return as-is if no normalization needed
	return unit
}

// replaceTemplateVariablesWithAutoDetection automatically detects index tags and replaces placeholders
func (dt *DefinitionBasedTransformer) replaceTemplateVariablesWithAutoDetection(template string, tags map[string]string) string {
	result := template
	
	// Detect all index tags automatically
	indexTags := dt.detectIndexTags(tags)
	
	dt.logger.Debug().
		Interface("detected_index_tags", indexTags).
		Str("template", template).
		Msg("Auto-detected index tags for template replacement")
	
	// Replace all detected index tags in the template
	for tagKey, tagValue := range indexTags {
		placeholder := fmt.Sprintf("{%s}", tagKey)
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, tagValue)
			dt.logger.Debug().
				Str("placeholder", placeholder).
				Str("value", tagValue).
				Msg("Auto-replaced template variable")
		}
	}
	
	// Special handling for generic {instance} placeholder
	if strings.Contains(result, "{instance}") {
		// Try to find the best index tag for {instance}
		instanceValue := dt.findBestInstanceTag(indexTags)
		if instanceValue != "" {
			result = strings.ReplaceAll(result, "{instance}", instanceValue)
			dt.logger.Debug().
				Str("instance_value", instanceValue).
				Msg("Replaced generic {instance} placeholder")
		}
	}
	
	return result
}

// detectIndexTags automatically identifies which tags are likely to be index/identifier tags
func (dt *DefinitionBasedTransformer) detectIndexTags(tags map[string]string) map[string]string {
	indexTags := make(map[string]string)
	
	for key, value := range tags {
		if dt.isIndexTag(key, value) {
			indexTags[key] = value
		}
	}
	
	return indexTags
}

// isIndexTag determines if a tag key/value pair represents an index or identifier
func (dt *DefinitionBasedTransformer) isIndexTag(key, value string) bool {
	// Skip system/metadata tags
	systemTags := []string{"host", "os", "platform", "probe_name", "state", "manufacturer", "model", "serial_number"}
	for _, systemTag := range systemTags {
		if key == systemTag {
			return false
		}
	}
	
	// Skip very long values (probably not indexes)
	if len(value) > 50 {
		return false
	}
	
	// CPU probe patterns (OS-specific)
	if key == "core" && dt.isNumeric(value) {
		return true // Unix/macOS: core="0", "1", "2"
	}
	if key == "instance" && (dt.isNumeric(value) || value == "_Total") {
		return true // Windows: instance="0", "1", "_Total"
	}
	
	// Network probe patterns
	if key == "interface" && len(value) <= 20 {
		return true // interface="en0", "Wi-Fi", "Ethernet"
	}
	if key == "adapter" && len(value) <= 30 {
		return true // Windows adapter names
	}
	
	// Redfish probe patterns
	redfishIndexTags := []string{"controller_id", "drive_id", "system_id", "controller"}
	for _, redfishTag := range redfishIndexTags {
		if key == redfishTag {
			return true
		}
	}
	
	// Physical positions
	physicalTags := []string{"slot", "channel", "socket", "bay"}
	for _, physicalTag := range physicalTags {
		if key == physicalTag && dt.isNumeric(value) {
			return true
		}
	}
	
	// Memory-specific tags
	if key == "memory_controller" && dt.isNumeric(value) {
		return true
	}
	
	// Generic numeric values for potential indexes
	if dt.isNumeric(value) && len(value) <= 3 {
		// Short numeric values are likely indexes
		return true
	}
	
	// Single letter values (like controller letters A, B, C)
	if len(value) == 1 && dt.isAlpha(value) {
		return true
	}
	
	return false
}

// findBestInstanceTag finds the most appropriate tag value for a generic {instance} placeholder
func (dt *DefinitionBasedTransformer) findBestInstanceTag(indexTags map[string]string) string {
	// Preference order for {instance} replacement
	preferenceOrder := []string{"core", "instance", "interface", "controller_id", "drive_id", "slot", "channel"}
	
	for _, preferred := range preferenceOrder {
		if value, exists := indexTags[preferred]; exists {
			return value
		}
	}
	
	// If no preferred tag found, return the first available index tag
	for _, value := range indexTags {
		return value
	}
	
	return ""
}

// isNumeric checks if a string contains only digits
func (dt *DefinitionBasedTransformer) isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// isAlpha checks if a string contains only letters
func (dt *DefinitionBasedTransformer) isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, char := range s {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
			return false
		}
	}
	return true
}
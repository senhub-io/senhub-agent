// senhub-agent/internal/agent/services/data_store/transformers/transformer.go
package transformers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/services/logger"
)

// MetricTransformer defines the interface for transforming metric names
type MetricTransformer interface {
	TransformMetricName(key string, tags map[string]string) string
	GetUnit(key string) string
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
			Msg("Definition-based transformer loaded successfully")
		return transformer, nil
	}

	// Fallback to legacy transformer
	tr.logger.Debug().
		Err(err).
		Str("probe", probeName).
		Msg("Definition-based transformer not found, falling back to legacy")
		
	transformer, err = tr.loadTransformerFromFile(probeName, style)
	if err != nil {
		tr.logger.Error().
			Err(err).
			Str("probe", probeName).
			Str("style", style).
			Msg("Failed to load transformer")
		return nil, err
	}

	// Cache the transformer
	tr.transformers[key] = transformer
	tr.logger.Debug().
		Str("probe", probeName).
		Str("style", style).
		Msg("Legacy transformer loaded successfully")

	return transformer, nil
}

// loadTransformerFromFile loads a transformer configuration from YAML file
func (tr *TransformerRegistry) loadTransformerFromFile(probeName, style string) (MetricTransformer, error) {
	filename := fmt.Sprintf("%s_%s.yaml", probeName, style)
	
	// Get the directory where this source file is located
	_, sourceFile, _, _ := runtime.Caller(0)
	transformersDir := filepath.Dir(sourceFile)
	filePath := filepath.Join(transformersDir, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		tr.logger.Warn().
			Str("file", filePath).
			Msg("Transformer file not found, using fallback")
		return tr.createFallbackTransformer(probeName, style), nil
	}

	// Read YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read transformer file %s: %w", filePath, err)
	}

	// Parse YAML
	var config TransformConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse transformer file %s: %w", filePath, err)
	}

	return &ProbeTransformer{
		probeName: probeName,
		style:     style,
		config:    config,
		logger:    tr.logger,
	}, nil
}

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
	// Basic transformation: replace dots and underscores with spaces, capitalize
	readable := strings.ReplaceAll(key, ".", " ")
	readable = strings.ReplaceAll(readable, "_", " ")
	
	// Capitalize first letter of each word
	words := strings.Fields(readable)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	
	return strings.Join(words, " ")
}

// loadDefinitionBasedTransformer loads a new definition-based transformer
func (tr *TransformerRegistry) loadDefinitionBasedTransformer(probeName string) (MetricTransformer, error) {
	// Get the directory where definitions are stored
	_, sourceFile, _, _ := runtime.Caller(0)
	transformersDir := filepath.Dir(sourceFile)
	definitionsDir := filepath.Join(transformersDir, "definitions")
	
	// Load probe definition
	probeFilePath := filepath.Join(definitionsDir, fmt.Sprintf("%s.yaml", probeName))
	definition, err := tr.loadProbeDefinition(probeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load probe definition: %w", err)
	}
	
	// Load shared configurations
	unitsConfig, err := tr.loadUnitsConfig(filepath.Join(definitionsDir, "shared", "units.yaml"))
	if err != nil {
		tr.logger.Warn().Err(err).Msg("Failed to load units config, using empty config")
		unitsConfig = &UnitsConfig{Units: make(map[string]UnitDefinition)}
	}
	
	templatesConfig, err := tr.loadTemplatesConfig(filepath.Join(definitionsDir, "shared", "templates.yaml"))
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
		// Convert pattern to regex-like matching
		regexPattern := pattern
		regexPattern = strings.ReplaceAll(regexPattern, "{index}", `[0-9]+`)
		regexPattern = strings.ReplaceAll(regexPattern, "{component}", `[^.]+`)
		regexPattern = strings.ReplaceAll(regexPattern, "{pool_id}", `[^.]+`)
		regexPattern = strings.ReplaceAll(regexPattern, ".", `\.`)
		
		// Simple prefix/suffix matching for now
		if strings.HasPrefix(regexPattern, text[:len(text)/2]) {
			return true
		}
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
	
	// Replace template variables with tag values
	displayName = dt.replaceTemplateVariables(displayName, tags)
	
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
	}
	
	for _, varName := range templateVars {
		placeholder := fmt.Sprintf("{%s}", varName)
		if strings.Contains(result, placeholder) {
			value := tags[varName]
			if value == "" {
				// Fallback values
				switch varName {
				case "index":
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
	// Remove common prefixes
	readable := metricName
	prefixes := []string{"dell_powervault_me.", "thermal.", "power.", "storage."}
	for _, prefix := range prefixes {
		readable = strings.TrimPrefix(readable, prefix)
	}
	
	// Replace dots and underscores with spaces
	readable = strings.ReplaceAll(readable, ".", " ")
	readable = strings.ReplaceAll(readable, "_", " ")
	
	// Capitalize first letter of each word
	words := strings.Fields(readable)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
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
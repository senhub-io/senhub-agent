// senhub-agent/internal/agent/services/data_store/transformers/transformer.go
package transformers

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/services/logger"
)

//go:embed definitions/*.yaml definitions/shared/*.yaml lookups/*.lookup corrections/*.yaml
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
	Name                   string            `yaml:"name"`
	Channel                string            `yaml:"channel"`
	DisplayName            string            `yaml:"display_name"`
	Unit                   string            `yaml:"unit"`
	MultiInstanceLabels    []string          `yaml:"multi_instance_labels"`
	TagFilter              map[string]string `yaml:"tag_filter"`
	Description            string            `yaml:"description"`
	CalculatedMetric       string            `yaml:"calculated_metric"`
	AlertThresholdWarning  int               `yaml:"alert_threshold_warning"`
	AlertThresholdCritical int               `yaml:"alert_threshold_critical"`
	Lookup                 string            `yaml:"lookup"`
}

// UnitCorrection represents a correction rule for inconsistent source data
type UnitCorrection struct {
	MetricPattern    string            `yaml:"metric_pattern"`
	VendorFilter     map[string]string `yaml:"vendor_filter"`
	DetectionRule    string            `yaml:"detection_rule"`
	CorrectionFactor float64           `yaml:"correction_factor"`
	Reason           string            `yaml:"reason"`
	Enabled          bool              `yaml:"enabled"`
}

// CorrectionsConfig represents the structure of a corrections configuration file
type CorrectionsConfig struct {
	Vendor      string           `yaml:"vendor"`
	ProductLine string           `yaml:"product_line"`
	Models      []string         `yaml:"models"`
	Corrections []UnitCorrection `yaml:"corrections"`
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
	Templates        map[string]string `yaml:"templates"`
	MetricTypes      map[string]string `yaml:"metric_types"`
	TagPriority      map[int][]string  `yaml:"tag_priority"`
	FallbackPatterns map[string]string `yaml:"fallback_patterns"`
}

// ProbeTransformer implements MetricTransformer for a specific probe and style (legacy)
type ProbeTransformer struct {
	probeName    string
	style        string
	config       TransformConfig
	moduleLogger *logger.ModuleLogger
}

// DefinitionBasedTransformer implements MetricTransformer using YAML definitions
type DefinitionBasedTransformer struct {
	probeName         string
	definition        *ProbeDefinition
	unitsConfig       *UnitsConfig
	templatesConfig   *TemplateConfig
	correctionsConfig *CorrectionsConfig
	moduleLogger      *logger.ModuleLogger
}

// TransformerRegistry manages all transformers
type TransformerRegistry struct {
	transformers map[string]MetricTransformer // key: "probe_name:style"
	moduleLogger *logger.ModuleLogger
}

// NewTransformerRegistry creates a new transformer registry
func NewTransformerRegistry(baseLogger *logger.Logger) *TransformerRegistry {
	// Create module-specific logger for transformer registry
	moduleLogger := logger.NewModuleLogger(baseLogger, "transformer")
	return &TransformerRegistry{
		transformers: make(map[string]MetricTransformer),
		moduleLogger: moduleLogger,
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
		tr.moduleLogger.Debug().
			Str("probe", probeName).
			Str("style", style).
			Msg("✅ Definition-based transformer loaded successfully")
		return transformer, nil
	}

	// Log the error and create fallback transformer directly
	tr.moduleLogger.Warn().
		Err(err).
		Str("probe", probeName).
		Msg("❌ Definition-based transformer not found, creating fallback")

	// Create fallback transformer directly instead of loading from file
	transformer = tr.createFallbackTransformer(probeName, style)

	// Cache the transformer
	tr.transformers[key] = transformer
	tr.moduleLogger.Debug().
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
		moduleLogger: tr.moduleLogger,
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

	// Replace any other template variables using tag values
	// This handles variables like {sensor_name}, {drive_name}, {volume_name}, etc.
	for tagKey, tagValue := range tags {
		placeholder := fmt.Sprintf("{%s}", tagKey)
		if strings.Contains(result, placeholder) {
			if tagValue == "" || tagValue == tagKey {
				// If tag value is empty or equals the tag key (e.g., sensor_name="sensor_name"),
				// use a generic fallback
				tagValue = "Unknown"
			}
			result = strings.ReplaceAll(result, placeholder, tagValue)
		}
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
	readable = strings.TrimPrefix(readable, "hardware.")

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
	tr.moduleLogger.Debug().
		Str("probe", probeName).
		Str("file_path", probeFilePath).
		Msg("🔍 Loading probe definition from embedded files")

	definition, err := tr.loadProbeDefinitionFromEmbed(probeFilePath)
	if err != nil {
		tr.moduleLogger.Error().
			Err(err).
			Str("probe", probeName).
			Str("file_path", probeFilePath).
			Msg("❌ Failed to load embedded probe definition")
		return nil, fmt.Errorf("failed to load probe definition: %w", err)
	}

	tr.moduleLogger.Debug().
		Str("probe", probeName).
		Int("metrics_count", len(definition.Metrics)).
		Msg("✅ Probe definition loaded from embedded files")

	// Load shared configurations from embedded files
	unitsConfig, err := tr.loadUnitsConfigFromEmbed("definitions/shared/units.yaml")
	if err != nil {
		tr.moduleLogger.Warn().Err(err).Msg("Failed to load units config, using empty config")
		unitsConfig = &UnitsConfig{Units: make(map[string]UnitDefinition)}
	}

	templatesConfig, err := tr.loadTemplatesConfigFromEmbed("definitions/shared/templates.yaml")
	if err != nil {
		tr.moduleLogger.Warn().Err(err).Msg("Failed to load templates config, using empty config")
		templatesConfig = &TemplateConfig{
			Templates:        make(map[string]string),
			MetricTypes:      make(map[string]string),
			TagPriority:      make(map[int][]string),
			FallbackPatterns: make(map[string]string),
		}
	}

	// Load corrections config for vendor-specific fixes (optional)
	correctionsConfig, err := tr.loadCorrectionsConfigFromEmbed(probeName)
	if err != nil {
		tr.moduleLogger.Debug().
			Err(err).
			Str("probe", probeName).
			Msg("No corrections config found (this is optional)")
		correctionsConfig = nil // No corrections available
	}

	// Create child module logger for definition-based transformer
	childLogger := logger.NewModuleLogger(tr.moduleLogger.Logger, "transformer.definition")

	return &DefinitionBasedTransformer{
		probeName:         probeName,
		definition:        definition,
		unitsConfig:       unitsConfig,
		templatesConfig:   templatesConfig,
		correctionsConfig: correctionsConfig,
		moduleLogger:      childLogger,
	}, nil
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

// loadCorrectionsConfigFromEmbed loads corrections configuration from embedded YAML file
func (tr *TransformerRegistry) loadCorrectionsConfigFromEmbed(probeName string) (*CorrectionsConfig, error) {
	// Try different correction file patterns based on probe name
	patterns := []string{
		fmt.Sprintf("corrections/%s.yaml", probeName),
		fmt.Sprintf("corrections/%s_corrections.yaml", probeName),
	}

	// For redfish probe, also try vendor-specific corrections
	if probeName == "redfish" {
		patterns = append(patterns,
			"corrections/dell_powervault_me.yaml",
			"corrections/hpe_smartarray.yaml",
			"corrections/lenovo_thinkagile.yaml",
		)
	}

	var lastErr error
	for _, pattern := range patterns {
		data, err := definitionFiles.ReadFile(pattern)
		if err != nil {
			lastErr = err
			continue
		}

		var config CorrectionsConfig
		err = yaml.Unmarshal(data, &config)
		if err != nil {
			lastErr = err
			continue
		}

		tr.moduleLogger.Debug().
			Str("probe", probeName).
			Str("corrections_file", pattern).
			Int("corrections_count", len(config.Corrections)).
			Msg("✅ Corrections config loaded")

		return &config, nil
	}

	return nil, fmt.Errorf("no corrections config found for probe %s: %w", probeName, lastErr)
}

// TransformMetricName implements MetricTransformer interface for definition-based transformer

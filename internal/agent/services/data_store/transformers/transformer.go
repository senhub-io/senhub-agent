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

// TransformConfig represents the structure of a transformation YAML file
type TransformConfig struct {
	Patterns map[string]string `yaml:"patterns"`
	Units    map[string]string `yaml:"units"`
}

// ProbeTransformer implements MetricTransformer for a specific probe and style
type ProbeTransformer struct {
	probeName string
	style     string
	config    TransformConfig
	logger    *logger.Logger
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

	// Load transformer from file
	transformer, err := tr.loadTransformerFromFile(probeName, style)
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
		Msg("Transformer loaded successfully")

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
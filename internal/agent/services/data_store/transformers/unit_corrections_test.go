// unit_corrections_test.go
package transformers

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestDefinitionBasedTransformer_ApplyUnitCorrection(t *testing.T) {
	// Create a logger for testing
	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).With().Timestamp().Logger()
	moduleLogger := logger.NewModuleLogger(&baseLogger, "transformer.test")

	tests := []struct {
		name              string
		metricName        string
		value             float64
		tags              map[string]string
		correctionsConfig *CorrectionsConfig
		expectedValue     float64
		expectedApplied   bool
	}{
		{
			name:       "Dell PowerVault ME capacity correction",
			metricName: "hardware.storage.volume.capacity.total",
			value:      15350213115904.0, // 15.35 TB (wrong scale)
			tags: map[string]string{
				"manufacturer": "Dell",
				"model":        "PowerVault ME5024",
			},
			correctionsConfig: &CorrectionsConfig{
				Vendor:      "Dell",
				ProductLine: "PowerVault ME",
				Corrections: []UnitCorrection{
					{
						MetricPattern:    "hardware.storage.*.capacity.*",
						VendorFilter:     map[string]string{"manufacturer": "Dell", "model": "PowerVault"},
						DetectionRule:    "value > 1e13",
						CorrectionFactor: 0.001, // Divide by 1024²
						Reason:           "Dell ME API reports capacity values in wrong unit scale",
						Enabled:          true,
					},
				},
			},
			expectedValue:   15350213115.904001, // Corrected value (~15.35 GB) - 15350213115904 * 0.001
			expectedApplied: true,
		},
		{
			name:       "No correction needed - small value",
			metricName: "hardware.storage.volume.capacity.total",
			value:      1073741824.0, // 1 GB
			tags: map[string]string{
				"manufacturer": "Dell",
				"model":        "PowerVault ME5024",
			},
			correctionsConfig: &CorrectionsConfig{
				Vendor:      "Dell",
				ProductLine: "PowerVault ME",
				Corrections: []UnitCorrection{
					{
						MetricPattern:    "hardware.storage.*.capacity.*",
						VendorFilter:     map[string]string{"manufacturer": "Dell", "model": "PowerVault"},
						DetectionRule:    "value > 1e13",
						CorrectionFactor: 0.001,
						Reason:           "Dell ME API reports capacity values in wrong unit scale",
						Enabled:          true,
					},
				},
			},
			expectedValue:   1073741824.0, // No change
			expectedApplied: false,
		},
		{
			name:       "No correction - wrong vendor",
			metricName: "hardware.storage.volume.capacity.total",
			value:      15350213115904.0,
			tags: map[string]string{
				"manufacturer": "HPE",
				"model":        "SmartArray",
			},
			correctionsConfig: &CorrectionsConfig{
				Vendor:      "Dell",
				ProductLine: "PowerVault ME",
				Corrections: []UnitCorrection{
					{
						MetricPattern:    "hardware.storage.*.capacity.*",
						VendorFilter:     map[string]string{"manufacturer": "Dell", "model": "PowerVault"},
						DetectionRule:    "value > 1e13",
						CorrectionFactor: 0.001,
						Reason:           "Dell ME API reports capacity values in wrong unit scale",
						Enabled:          true,
					},
				},
			},
			expectedValue:   15350213115904.0, // No change
			expectedApplied: false,
		},
		{
			name:       "No correction - disabled",
			metricName: "hardware.storage.volume.capacity.total",
			value:      15350213115904.0,
			tags: map[string]string{
				"manufacturer": "Dell",
				"model":        "PowerVault ME5024",
			},
			correctionsConfig: &CorrectionsConfig{
				Vendor:      "Dell",
				ProductLine: "PowerVault ME",
				Corrections: []UnitCorrection{
					{
						MetricPattern:    "hardware.storage.*.capacity.*",
						VendorFilter:     map[string]string{"manufacturer": "Dell", "model": "PowerVault"},
						DetectionRule:    "value > 1e13",
						CorrectionFactor: 0.001,
						Reason:           "Dell ME API reports capacity values in wrong unit scale",
						Enabled:          false, // Disabled
					},
				},
			},
			expectedValue:   15350213115904.0, // No change
			expectedApplied: false,
		},
		{
			name:       "Speed correction - Mbps to Gbps",
			metricName: "hardware.storage.drive.speed_gbs",
			value:      12000.0, // 12000 Mbps
			tags: map[string]string{
				"manufacturer": "Dell",
			},
			correctionsConfig: &CorrectionsConfig{
				Vendor:      "Dell",
				ProductLine: "PowerVault ME",
				Corrections: []UnitCorrection{
					{
						MetricPattern:    "hardware.storage.drive.speed_gbs",
						VendorFilter:     map[string]string{"manufacturer": "Dell"},
						DetectionRule:    "value > 1000",
						CorrectionFactor: 0.001, // Convert Mbps to Gbps
						Reason:           "Speed values reported in wrong unit",
						Enabled:          true,
					},
				},
			},
			expectedValue:   12.0, // 12 Gbps
			expectedApplied: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create transformer with corrections config
			transformer := &DefinitionBasedTransformer{
				probeName:         "redfish",
				correctionsConfig: tt.correctionsConfig,
				moduleLogger:      moduleLogger,
			}

			// Apply correction
			correctedValue, applied := transformer.ApplyUnitCorrection(tt.metricName, tt.value, tt.tags)

			// Debug output for failing case
			if tt.name == "Dell PowerVault ME capacity correction" && !applied {
				t.Logf("DEBUG: Expected correction to be applied but it wasn't")
				t.Logf("  metricName: %s", tt.metricName)
				t.Logf("  value: %f", tt.value)
				t.Logf("  tags: %+v", tt.tags)
				if tt.correctionsConfig != nil && len(tt.correctionsConfig.Corrections) > 0 {
					correction := tt.correctionsConfig.Corrections[0]
					t.Logf("  correction pattern: %s", correction.MetricPattern)
					t.Logf("  correction vendor filter: %+v", correction.VendorFilter)
					t.Logf("  correction detection rule: %s", correction.DetectionRule)
					t.Logf("  correction enabled: %v", correction.Enabled)

					// Test each step of the correction process manually
					// Pattern matching test
					patternMatches := testMatchesPattern(correction.MetricPattern, tt.metricName)
					vendorMatches := testMatchesVendorFilter(correction.VendorFilter, tt.tags)
					detectionMatches := testMatchesDetectionRule(correction.DetectionRule, tt.value)

					t.Logf("  pattern matches: %v", patternMatches)
					t.Logf("  vendor filter matches: %v", vendorMatches)
					t.Logf("  detection rule matches: %v", detectionMatches)
				}
			}

			// Check if correction was applied as expected
			if applied != tt.expectedApplied {
				t.Errorf("Expected applied=%v, got applied=%v", tt.expectedApplied, applied)
			}

			// Check corrected value (with some tolerance for floating point precision)
			tolerance := 0.001
			if abs(correctedValue-tt.expectedValue) > tolerance {
				t.Errorf("Expected value=%.3f, got value=%.3f", tt.expectedValue, correctedValue)
			}
		})
	}
}

func TestDefinitionBasedTransformer_matchesVendorFilter(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).With().Timestamp().Logger()
	moduleLogger := logger.NewModuleLogger(&baseLogger, "transformer.test")
	transformer := &DefinitionBasedTransformer{
		moduleLogger: moduleLogger,
	}

	tests := []struct {
		name     string
		filter   map[string]string
		tags     map[string]string
		expected bool
	}{
		{
			name:   "Empty filter matches all",
			filter: map[string]string{},
			tags: map[string]string{
				"manufacturer": "Dell",
			},
			expected: true,
		},
		{
			name: "Exact match",
			filter: map[string]string{
				"manufacturer": "Dell",
			},
			tags: map[string]string{
				"manufacturer": "Dell",
			},
			expected: true,
		},
		{
			name: "Partial match",
			filter: map[string]string{
				"model": "PowerVault",
			},
			tags: map[string]string{
				"model": "PowerVault ME5024",
			},
			expected: true,
		},
		{
			name: "No match",
			filter: map[string]string{
				"manufacturer": "Dell",
			},
			tags: map[string]string{
				"manufacturer": "HPE",
			},
			expected: false,
		},
		{
			name: "Missing tag",
			filter: map[string]string{
				"manufacturer": "Dell",
			},
			tags: map[string]string{
				"model": "PowerVault",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.matchesVendorFilter(tt.filter, tt.tags)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDefinitionBasedTransformer_matchesDetectionRule(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := zerolog.New(&buf).With().Timestamp().Logger()
	moduleLogger := logger.NewModuleLogger(&baseLogger, "transformer.test")
	transformer := &DefinitionBasedTransformer{
		moduleLogger: moduleLogger,
	}

	tests := []struct {
		name     string
		rule     string
		value    float64
		expected bool
	}{
		{
			name:     "Always rule",
			rule:     "always",
			value:    100.0,
			expected: true,
		},
		{
			name:     "Never rule",
			rule:     "never",
			value:    100.0,
			expected: false,
		},
		{
			name:     "Value greater than - true",
			rule:     "value > 1e15",
			value:    1.5e15,
			expected: true,
		},
		{
			name:     "Value greater than - false",
			rule:     "value > 1e15",
			value:    1e14,
			expected: false,
		},
		{
			name:     "Value less than - true",
			rule:     "value < 100",
			value:    50.0,
			expected: true,
		},
		{
			name:     "Value less than - false",
			rule:     "value < 100",
			value:    150.0,
			expected: false,
		},
		{
			name:     "Value equals - true",
			rule:     "value == 100",
			value:    100.0,
			expected: true,
		},
		{
			name:     "Value equals - false",
			rule:     "value == 100",
			value:    101.0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.matchesDetectionRule(tt.rule, tt.value)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper function for floating point comparison
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Helper test functions to debug the correction process
func testMatchesPattern(pattern, text string) bool {
	// Simple exact match
	if pattern == text {
		return true
	}

	// Handle wildcards like {index}, {component}, etc.
	if strings.Contains(pattern, "{") {
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

			// If it's a wildcard "*", accept any value
			if patternPart == "*" {
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

func testMatchesVendorFilter(filter map[string]string, tags map[string]string) bool {
	if len(filter) == 0 {
		return true // No filter means match all
	}

	// All filter conditions must match
	for filterKey, filterValue := range filter {
		tagValue, exists := tags[filterKey]
		if !exists {
			return false
		}

		// Check if tag value contains the filter value (partial match)
		if !strings.Contains(strings.ToLower(tagValue), strings.ToLower(filterValue)) {
			return false
		}
	}

	return true
}

func testMatchesDetectionRule(rule string, value float64) bool {
	switch {
	case rule == "always":
		return true
	case strings.HasPrefix(rule, "value > "):
		threshold := parseTestThreshold(rule[8:])
		return value > threshold
	case strings.HasPrefix(rule, "value < "):
		threshold := parseTestThreshold(rule[8:])
		return value < threshold
	case strings.HasPrefix(rule, "value == "):
		threshold := parseTestThreshold(rule[9:])
		return value == threshold
	case rule == "never":
		return false
	default:
		return false
	}
}

func parseTestThreshold(threshold string) float64 {
	if val, err := strconv.ParseFloat(threshold, 64); err == nil {
		return val
	}
	return 0
}

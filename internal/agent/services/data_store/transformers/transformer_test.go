// senhub-agent/internal/agent/services/data_store/transformers/transformer_test.go
package transformers

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func createTestLogger() *logger.Logger {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	return logger.NewLogger(args)
}

func TestNewTransformerRegistry(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)

	if registry == nil {
		t.Fatal("Expected registry to be created, got nil")
	}

	if registry.transformers == nil {
		t.Fatal("Expected transformers map to be initialized")
	}
}

func TestLoadTransformer(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)

	tests := []struct {
		name      string
		probeName string
		style     string
		wantError bool
	}{
		{
			name:      "Load redfish friendly transformer",
			probeName: "redfish",
			style:     "friendly",
			wantError: false,
		},
		{
			name:      "Load host friendly transformer",
			probeName: "host", 
			style:     "friendly",
			wantError: false,
		},
		{
			name:      "Load non-existent transformer (should create fallback)",
			probeName: "nonexistent",
			style:     "unknown",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer, err := registry.LoadTransformer(tt.probeName, tt.style)

			if tt.wantError && err == nil {
				t.Errorf("Expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
			if transformer == nil {
				t.Errorf("Expected transformer to be created, got nil")
			}
		})
	}
}

func TestTransformerCaching(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)

	// Load transformer twice
	transformer1, err1 := registry.LoadTransformer("redfish", "friendly")
	transformer2, err2 := registry.LoadTransformer("redfish", "friendly")

	if err1 != nil || err2 != nil {
		t.Fatalf("Unexpected errors: %v, %v", err1, err2)
	}

	// Should return the same instance (cached)
	if transformer1 != transformer2 {
		t.Error("Expected same transformer instance from cache")
	}

	// Verify cache contains the entry
	if len(registry.transformers) != 1 {
		t.Errorf("Expected 1 cached transformer, got %d", len(registry.transformers))
	}
}

func TestProbeTransformer_TransformMetricName(t *testing.T) {
	logger := createTestLogger()

	// Create transformer with test patterns
	transformer := &ProbeTransformer{
		probeName: "test",
		style:     "friendly",
		config: TransformConfig{
			Patterns: map[string]string{
				"thermal.cpu.{index}.temperature": "CPU Temperature - Processor {index}",
				"memory.used_percent":              "Memory Usage",
				"power.psu.{index}.output_watts":   "Power Supply - PSU{index} Output",
			},
		},
		logger: logger,
	}

	tests := []struct {
		name     string
		key      string
		tags     map[string]string
		expected string
	}{
		{
			name: "Transform CPU temperature with index",
			key:  "thermal.cpu.0.temperature",
			tags: map[string]string{"index": "0"},
			expected: "CPU Temperature - Processor 0",
		},
		{
			name: "Transform memory usage",
			key:  "memory.used_percent",
			tags: map[string]string{},
			expected: "Memory Usage",
		},
		{
			name: "Transform PSU power with index",
			key:  "power.psu.1.output_watts",
			tags: map[string]string{"index": "1"},
			expected: "Power Supply - PSU1 Output",
		},
		{
			name: "Unknown metric fallback",
			key:  "unknown.metric.name",
			tags: map[string]string{},
			expected: "Unknown Metric Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.TransformMetricName(tt.key, tt.tags)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestProbeTransformer_GetUnit(t *testing.T) {
	logger := createTestLogger()

	transformer := &ProbeTransformer{
		probeName: "test",
		style:     "friendly",
		config: TransformConfig{
			Units: map[string]string{
				"temperature": "°C",
				"power":       "W",
				"percentage":  "%",
			},
		},
		logger: logger,
	}

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "Get temperature unit",
			key:      "temperature",
			expected: "°C",
		},
		{
			name:     "Get power unit",
			key:      "power",
			expected: "W",
		},
		{
			name:     "Get unknown unit",
			key:      "unknown",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.GetUnit(tt.key)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	logger := createTestLogger()
	transformer := &ProbeTransformer{logger: logger}

	tests := []struct {
		name     string
		key      string
		pattern  string
		expected bool
	}{
		{
			name:     "Exact match",
			key:      "memory.used_percent",
			pattern:  "memory.used_percent",
			expected: true,
		},
		{
			name:     "Pattern with wildcard prefix match",
			key:      "thermal.cpu.0.temperature",
			pattern:  "thermal.cpu.{index}.temperature",
			expected: true,
		},
		{
			name:     "Pattern with wildcard no match",
			key:      "power.psu.1.voltage",
			pattern:  "thermal.cpu.{index}.temperature",
			expected: false,
		},
		{
			name:     "No wildcard no match",
			key:      "memory.free_percent",
			pattern:  "memory.used_percent",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.matchesPattern(tt.key, tt.pattern)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestApplyTemplate(t *testing.T) {
	logger := createTestLogger()
	transformer := &ProbeTransformer{logger: logger}

	tests := []struct {
		name     string
		template string
		tags     map[string]string
		expected string
	}{
		{
			name:     "Template with index",
			template: "CPU Temperature - Processor {index}",
			tags:     map[string]string{"index": "1"},
			expected: "CPU Temperature - Processor 1",
		},
		{
			name:     "Template with component",
			template: "{component} Health Status",
			tags:     map[string]string{"component": "Memory"},
			expected: "Memory Health Status",
		},
		{
			name:     "Template with missing index (fallback to 0)",
			template: "PSU {index} Output",
			tags:     map[string]string{},
			expected: "PSU 0 Output",
		},
		{
			name:     "Template with missing component (fallback to Unknown)",
			template: "{component} Status",
			tags:     map[string]string{},
			expected: "Unknown Status",
		},
		{
			name:     "Template with instance tag",
			template: "Drive {index} Health",
			tags:     map[string]string{"instance": "sda"},
			expected: "Drive sda Health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.applyTemplate(tt.template, tt.tags)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestMakeReadable(t *testing.T) {
	logger := createTestLogger()
	transformer := &ProbeTransformer{logger: logger}

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "Dots and underscores",
			key:      "cpu.usage_percent",
			expected: "Cpu Usage Percent",
		},
		{
			name:     "Multiple dots",
			key:      "system.memory.used.bytes",
			expected: "System Memory Used Bytes",
		},
		{
			name:     "Already readable",
			key:      "Temperature",
			expected: "Temperature",
		},
		{
			name:     "Mixed separators",
			key:      "network.interface_rx.bytes_per_sec",
			expected: "Network Interface Rx Bytes Per Sec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.makeReadable(tt.key)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLoadTransformerFromFile(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)

	// Test loading existing file
	t.Run("Load existing redfish_friendly.yaml", func(t *testing.T) {
		transformer, err := registry.loadTransformerFromFile("redfish", "friendly")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		probeTransformer, ok := transformer.(*ProbeTransformer)
		if !ok {
			t.Fatal("Expected ProbeTransformer type")
		}

		// Check that some patterns were loaded
		if len(probeTransformer.config.Patterns) == 0 {
			t.Error("Expected patterns to be loaded from YAML file")
		}

		// Check specific pattern
		if pattern, exists := probeTransformer.config.Patterns["thermal.cpu.{index}.temperature"]; !exists {
			t.Error("Expected specific pattern to be loaded")
		} else if pattern != "CPU Temperature - Processor {index}" {
			t.Errorf("Expected specific pattern value, got %q", pattern)
		}

		// Check units
		if len(probeTransformer.config.Units) == 0 {
			t.Error("Expected units to be loaded from YAML file")
		}
	})

	// Test non-existent file (should create fallback)
	t.Run("Load non-existent file creates fallback", func(t *testing.T) {
		transformer, err := registry.loadTransformerFromFile("nonexistent", "style")
		if err != nil {
			t.Fatalf("Expected no error for fallback, got %v", err)
		}

		probeTransformer, ok := transformer.(*ProbeTransformer)
		if !ok {
			t.Fatal("Expected ProbeTransformer type")
		}

		// Fallback should have empty patterns
		if len(probeTransformer.config.Patterns) != 0 {
			t.Error("Expected fallback transformer to have empty patterns")
		}
	})
}

func TestCreateFallbackTransformer(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)

	transformer := registry.createFallbackTransformer("test", "style")
	
	probeTransformer, ok := transformer.(*ProbeTransformer)
	if !ok {
		t.Fatal("Expected ProbeTransformer type")
	}

	if probeTransformer.probeName != "test" {
		t.Errorf("Expected probe name 'test', got %q", probeTransformer.probeName)
	}

	if probeTransformer.style != "style" {
		t.Errorf("Expected style 'style', got %q", probeTransformer.style)
	}

	if len(probeTransformer.config.Patterns) != 0 {
		t.Error("Expected fallback to have empty patterns")
	}

	if len(probeTransformer.config.Units) != 0 {
		t.Error("Expected fallback to have empty units")
	}
}
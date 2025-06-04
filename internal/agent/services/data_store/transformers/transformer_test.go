// senhub-agent/internal/agent/services/data_store/transformers/transformer_test.go
package transformers

import (
	"strings"
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
			name:      "Load cpu friendly transformer",
			probeName: "cpu", 
			style:     "friendly",
			wantError: false,
		},
		{
			name:      "Load memory friendly transformer",
			probeName: "memory", 
			style:     "friendly",
			wantError: false,
		},
		{
			name:      "Load network friendly transformer",
			probeName: "network", 
			style:     "friendly",
			wantError: false,
		},
		{
			name:      "Load logicaldisk friendly transformer",
			probeName: "logicaldisk", 
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
			expected: "CPU Usage Percent",
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
		{
			name:     "IO transformation",
			key:      "disk.io.operations",
			expected: "Disk IO Operations",
		},
		{
			name:     "IO with mixed case",
			key:      "system_io_wait",
			expected: "System IO Wait",
		},
		{
			name:     "CPU iowait transformation",
			key:      "cpu_iowait",
			expected: "CPU IO Wait",
		},
		{
			name:     "Redfish hardware prefix removal",
			key:      "hardware.fan.health",
			expected: "Fan Health",
		},
		{
			name:     "Host disk prefix removal", 
			key:      "disk_free_percent",
			expected: "Free Percent",
		},
		{
			name:     "Host memory prefix removal",
			key:      "memory_available_bytes", 
			expected: "Available Bytes",
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
		transformer, err := registry.LoadTransformer("redfish", "friendly")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		definitionTransformer, ok := transformer.(*DefinitionBasedTransformer)
		if !ok {
			t.Fatal("Expected DefinitionBasedTransformer type")
		}

		// Check that some metrics were loaded
		if len(definitionTransformer.definition.Metrics) == 0 {
			t.Error("Expected metrics to be loaded from YAML file")
		}

		// Basic validation that transformer was created successfully
		if definitionTransformer.definition == nil {
			t.Error("Expected definition to be loaded")
		}
	})

	// Test non-existent file (should create fallback)
	t.Run("Load non-existent file creates fallback", func(t *testing.T) {
		transformer, err := registry.LoadTransformer("nonexistent", "style")
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

func TestReplaceTemplateVariablesWithDefinition(t *testing.T) {
	logger := createTestLogger()
	
	// Create test transformer with mock definition
	transformer := &DefinitionBasedTransformer{
		logger: logger,
	}
	
	tests := []struct {
		name       string
		template   string
		tags       map[string]string
		definition MetricDefinition
		expected   string
	}{
		{
			name:     "CPU core with multi_instance_labels",
			template: "CPU Core {core} Usage",
			tags:     map[string]string{"core": "1", "index": "999"},
			definition: MetricDefinition{
				MultiInstanceLabels: []string{"core"},
			},
			expected: "CPU Core 1 Usage",
		},
		{
			name:     "Disk with device from multi_instance_labels",
			template: "Disk {device} Free",
			tags:     map[string]string{"device": "C", "mountpoint": "/mount", "other": "ignored"},
			definition: MetricDefinition{
				MultiInstanceLabels: []string{"device", "mountpoint"},
			},
			expected: "Disk C Free",
		},
		{
			name:     "Network interface with multi_instance_labels priority",
			template: "Network {interface} Sent",
			tags:     map[string]string{"interface": "eth0", "device": "ignored_device"},
			definition: MetricDefinition{
				MultiInstanceLabels: []string{"interface"},
			},
			expected: "Network eth0 Sent",
		},
		{
			name:     "Multiple placeholders from multi_instance_labels",
			template: "Disk {device} at {mountpoint} Usage",
			tags:     map[string]string{"device": "sda1", "mountpoint": "/var/log"},
			definition: MetricDefinition{
				MultiInstanceLabels: []string{"device", "mountpoint"},
			},
			expected: "Disk sda1 at /var/log Usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.replaceTemplateVariablesWithDefinition(tt.template, tt.tags, tt.definition)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDefinitionBasedTransformer(t *testing.T) {
	logger := createTestLogger()
	registry := NewTransformerRegistry(logger)
	
	// Test metric transformation with tags for atomized probes
	tests := []struct {
		name       string
		probeName  string
		metricName string
		tags       map[string]string
		expectContains string
	}{
		{
			name:       "CPU usage with instance",
			probeName:  "cpu",
			metricName: "cpu_usage_total",
			tags: map[string]string{
				"probe_name": "cpu",
				"core":       "0",
			},
			expectContains: "CPU Total Usage",
		},
		{
			name:       "Memory usage",
			probeName:  "memory",
			metricName: "memory_used_percent",
			tags: map[string]string{
				"probe_name": "memory",
			},
			expectContains: "Memory Usage",
		},
		{
			name:       "Network interface",
			probeName:  "network",
			metricName: "bytes_sent",
			tags: map[string]string{
				"probe_name": "network",
				"interface":  "eth0",
			},
			expectContains: "Network eth0",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Load transformer for this specific probe
			transformer, err := registry.LoadTransformer(tt.probeName, "friendly")
			if err != nil {
				t.Logf("Definition-based transformer not loaded for %s, using fallback: %v", tt.probeName, err)
				// This is expected if definition files don't exist yet
				return
			}
			
			displayName := transformer.TransformMetricName(tt.metricName, tt.tags)
			
			if !strings.Contains(displayName, tt.expectContains) {
				t.Errorf("Expected display name to contain '%s', got '%s'", tt.expectContains, displayName)
			}
			
			t.Logf("Metric: %s -> Display: %s", tt.metricName, displayName)
		})
	}
}

// TestDefinitionBasedTransformer_AutoDetectIndexTags tests the automatic index tag detection system
func TestDefinitionBasedTransformer_AutoDetectIndexTags(t *testing.T) {
	logger := createTestLogger()
	
	// Create a definition-based transformer with a simple definition
	definition := &ProbeDefinition{
		Metrics: []MetricDefinition{
			{
				Name:        "cpu_core_usage",
				DisplayName: "CPU Core {instance} Usage",
				Unit:        "%",
				// No multi_instance_labels - should use auto-detection
			},
		},
	}
	
	transformer := &DefinitionBasedTransformer{
		probeName:       "test",
		definition:      definition,
		templatesConfig: &TemplateConfig{
			FallbackPatterns: map[string]string{
				"generic": "{metric_name}",
			},
		},
		logger: logger,
	}
	
	tests := []struct {
		name     string
		metric   string
		tags     map[string]string
		expected string
	}{
		{
			name:   "CPU core with Unix/macOS core tag",
			metric: "cpu_core_usage",
			tags: map[string]string{
				"core":       "6",
				"host":       "test-host",
				"os":         "darwin",
				"platform":   "darwin",
				"probe_name": "cpu",
			},
			expected: "CPU Core 6 Usage",
		},
		{
			name:   "CPU core with Windows instance tag",
			metric: "cpu_core_usage",
			tags: map[string]string{
				"instance":   "3",
				"host":       "test-host",
				"os":         "windows", 
				"platform":   "windows",
				"probe_name": "cpu",
			},
			expected: "CPU Core 3 Usage",
		},
		{
			name:   "Network interface with interface tag",
			metric: "network_bytes_sent",
			tags: map[string]string{
				"interface":  "en0",
				"host":       "test-host",
				"os":         "darwin",
				"probe_name": "network",
			},
			expected: "Network Bytes Sent", // No template, should use fallback
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.TransformMetricName(tt.metric, tt.tags)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
			t.Logf("Auto-detected transformation: %s -> %s", tt.metric, result)
		})
	}
}

// TestDefinitionBasedTransformer_DetectIndexTags tests the index tag detection logic
func TestDefinitionBasedTransformer_DetectIndexTags(t *testing.T) {
	logger := createTestLogger()
	transformer := &DefinitionBasedTransformer{
		probeName: "test",
		logger:    logger,
	}
	
	tests := []struct {
		name           string
		tags           map[string]string
		expectedIndexes map[string]string
	}{
		{
			name: "CPU probe Unix/macOS tags",
			tags: map[string]string{
				"core":       "6",
				"host":       "MacBook-Pro.local",
				"os":         "darwin",
				"platform":   "darwin",
				"probe_name": "cpu",
			},
			expectedIndexes: map[string]string{
				"core": "6",
			},
		},
		{
			name: "CPU probe Windows tags",
			tags: map[string]string{
				"instance":   "0",
				"host":       "WIN-PC",
				"os":         "windows",
				"platform":   "windows", 
				"probe_name": "cpu",
			},
			expectedIndexes: map[string]string{
				"instance": "0",
			},
		},
		{
			name: "Network probe tags",
			tags: map[string]string{
				"interface":  "en0",
				"ip":         "192.168.1.100",
				"host":       "test-host",
				"probe_name": "network",
			},
			expectedIndexes: map[string]string{
				"interface": "en0",
			},
		},
		{
			name: "Redfish probe tags",
			tags: map[string]string{
				"controller_id": "A",
				"drive_id":      "0",
				"slot":          "2",
				"manufacturer":  "Dell",
				"model":         "PowerVault",
				"probe_name":    "redfish",
			},
			expectedIndexes: map[string]string{
				"controller_id": "A",
				"drive_id":      "0", 
				"slot":          "2",
			},
		},
		{
			name: "System tags only (no indexes)",
			tags: map[string]string{
				"host":         "test-host",
				"os":           "linux",
				"platform":     "linux",
				"probe_name":   "system",
				"manufacturer": "Dell",
				"model":        "PowerEdge",
			},
			expectedIndexes: map[string]string{},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detected := transformer.detectIndexTags(tt.tags)
			
			if len(detected) != len(tt.expectedIndexes) {
				t.Errorf("Expected %d index tags, got %d", len(tt.expectedIndexes), len(detected))
			}
			
			for expectedKey, expectedValue := range tt.expectedIndexes {
				if detectedValue, exists := detected[expectedKey]; !exists {
					t.Errorf("Expected index tag '%s' not detected", expectedKey)
				} else if detectedValue != expectedValue {
					t.Errorf("Expected index tag '%s'='%s', got '%s'", expectedKey, expectedValue, detectedValue)
				}
			}
			
			t.Logf("Detected index tags: %v", detected)
		})
	}
}

// TestDefinitionBasedTransformer_FindBestInstanceTag tests the instance tag selection logic
func TestDefinitionBasedTransformer_FindBestInstanceTag(t *testing.T) {
	logger := createTestLogger()
	transformer := &DefinitionBasedTransformer{
		probeName: "test",
		logger:    logger,
	}
	
	tests := []struct {
		name      string
		indexTags map[string]string
		expected  string
	}{
		{
			name: "CPU core preferred over instance",
			indexTags: map[string]string{
				"core":     "6",
				"instance": "3",
			},
			expected: "6", // core has priority
		},
		{
			name: "Windows instance only",
			indexTags: map[string]string{
				"instance": "2",
			},
			expected: "2",
		},
		{
			name: "Network interface",
			indexTags: map[string]string{
				"interface": "eth0",
			},
			expected: "eth0",
		},
		{
			name: "Redfish controller preferred",
			indexTags: map[string]string{
				"slot":          "3", 
				"controller_id": "A",
				"drive_id":      "1",
			},
			expected: "A", // controller_id has priority
		},
		{
			name:      "No index tags",
			indexTags: map[string]string{},
			expected:  "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.findBestInstanceTag(tt.indexTags)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
			t.Logf("Best instance tag for %v: %s", tt.indexTags, result)
		})
	}
}
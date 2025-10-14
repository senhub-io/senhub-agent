package probes

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestGetProbeConstructorForConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      configuration.ProbeConfig
		shouldError bool
		description string
	}{
		{
			name: "Valid probe type - cpu",
			config: configuration.ProbeConfig{
				Name: "CPU Monitor",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldError: false,
			description: "Should return constructor for CPU probe",
		},
		{
			name: "Valid probe type - memory",
			config: configuration.ProbeConfig{
				Name: "Memory Monitor",
				Type: "memory",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldError: false,
			description: "Should return constructor for memory probe",
		},
		{
			name: "Valid probe type - citrix",
			config: configuration.ProbeConfig{
				Name: "Production Citrix",
				Type: "citrix",
				Params: map[string]interface{}{
					"base_url": "https://director.example.com",
					"interval": 120,
				},
			},
			shouldError: false,
			description: "Should return constructor for Citrix probe",
		},
		{
			name: "Valid probe type - redfish",
			config: configuration.ProbeConfig{
				Name: "Server Hardware",
				Type: "redfish",
				Params: map[string]interface{}{
					"endpoint": "https://server-bmc.example.com",
					"username": "admin",
					"password": "secret",
				},
			},
			shouldError: false,
			description: "Should return constructor for Redfish probe",
		},
		{
			name: "Invalid probe type",
			config: configuration.ProbeConfig{
				Name: "Unknown Probe",
				Type: "unknown_probe_type",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldError: true,
			description: "Should return error for unknown probe type",
		},
		{
			name: "Empty probe type",
			config: configuration.ProbeConfig{
				Name: "Probe Without Type",
				Type: "",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldError: true,
			description: "Should return error for empty probe type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constructor, err := getProbeConstructorForConfig(tt.config)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error, got nil. %s", tt.description)
				}
				if constructor != nil {
					t.Error("Expected nil constructor when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}
				if constructor == nil {
					t.Error("Expected non-nil constructor")
				}
			}
		})
	}
}

func TestGenerateProbeId(t *testing.T) {
	tests := []struct {
		name        string
		config1     configuration.ProbeConfig
		config2     configuration.ProbeConfig
		shouldMatch bool
		description string
	}{
		{
			name: "Identical configs produce same ID",
			config1: configuration.ProbeConfig{
				Name: "cpu",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			config2: configuration.ProbeConfig{
				Name: "cpu",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldMatch: true,
			description: "Same config should generate same ID",
		},
		{
			name: "Different names produce different IDs",
			config1: configuration.ProbeConfig{
				Name: "CPU Monitor 1",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			config2: configuration.ProbeConfig{
				Name: "CPU Monitor 2",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			shouldMatch: false,
			description: "Different names should produce different IDs",
		},
		{
			name: "Different params produce different IDs",
			config1: configuration.ProbeConfig{
				Name: "cpu",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 30,
				},
			},
			config2: configuration.ProbeConfig{
				Name: "cpu",
				Type: "cpu",
				Params: map[string]interface{}{
					"interval": 60,
				},
			},
			shouldMatch: false,
			description: "Different parameters should produce different IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := GenerateProbeId(tt.config1)
			id2 := GenerateProbeId(tt.config2)

			// IDs should never be empty
			if id1 == "" || id2 == "" {
				t.Error("Generated IDs should not be empty")
			}

			// IDs should be valid hex strings (SHA256 = 64 chars)
			if len(id1) != 64 || len(id2) != 64 {
				t.Errorf("IDs should be 64 characters (SHA256 hex), got %d and %d", len(id1), len(id2))
			}

			// Check if IDs match expectation
			if tt.shouldMatch {
				if id1 != id2 {
					t.Errorf("Expected matching IDs, got %s and %s. %s", id1, id2, tt.description)
				}
			} else {
				if id1 == id2 {
					t.Errorf("Expected different IDs, got same ID: %s. %s", id1, tt.description)
				}
			}
		})
	}
}

func TestProbeRegistry(t *testing.T) {
	// Test that all expected probes are registered
	expectedProbes := []string{
		"load_webapp",
		"ping_webapp",
		"ping_gateway",
		"wifi_signal_strength",
		"memory",
		"cpu",
		"network",
		"logicaldisk",
		"syslog",
		"event",
		"otel",
		"redfish",
		"citrix",
	}

	for _, probeName := range expectedProbes {
		t.Run("Check_"+probeName, func(t *testing.T) {
			constructor, exists := probeConstructors[probeName]
			if !exists {
				t.Errorf("Probe '%s' not found in registry", probeName)
			}
			if constructor == nil {
				t.Errorf("Constructor for probe '%s' is nil", probeName)
			}
		})
	}

	// Test that registry is not empty
	if len(probeConstructors) == 0 {
		t.Error("Probe registry should not be empty")
	}

	// Verify expected count
	expectedCount := len(expectedProbes)
	actualCount := len(probeConstructors)
	if actualCount != expectedCount {
		t.Errorf("Expected %d probes in registry, got %d", expectedCount, actualCount)
	}
}

func TestGenerateProbeId_Deterministic(t *testing.T) {
	// Test that the same config always produces the same ID (deterministic)
	config := configuration.ProbeConfig{
		Name: "test-probe",
		Type: "cpu",
		Params: map[string]interface{}{
			"interval": 30,
		},
	}

	// Generate ID 10 times
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = GenerateProbeId(config)
	}

	// All IDs should be identical
	firstID := ids[0]
	for i, id := range ids {
		if id != firstID {
			t.Errorf("ID generation is not deterministic: iteration %d produced different ID", i)
		}
	}
}

func TestGetProbeConstructorForConfig_AllRegisteredProbes(t *testing.T) {
	// Test that we can successfully get constructors for all registered probes
	for probeName := range probeConstructors {
		t.Run("Get_constructor_"+probeName, func(t *testing.T) {
			config := configuration.ProbeConfig{
				Name:   probeName + "-test",
				Type:   probeName,
				Params: map[string]interface{}{},
			}

			constructor, err := getProbeConstructorForConfig(config)
			if err != nil {
				t.Errorf("Failed to get constructor for registered probe '%s': %v", probeName, err)
			}
			if constructor == nil {
				t.Errorf("Constructor for registered probe '%s' is nil", probeName)
			}
		})
	}
}

func TestNewProbePoller_InvalidProbeType(t *testing.T) {
	// Test creating a probe poller with invalid probe type
	config := configuration.ProbeConfig{
		Name: "Invalid Probe",
		Type: "nonexistent_probe",
		Params: map[string]interface{}{
			"interval": 30,
		},
	}

	// Create mock logger with minimal args
	mockArgs := &cliArgs.ParsedArgs{
		DebugLogShipperUrl: "",
	}
	baseLogger := logger.NewLogger(mockArgs)

	// Create mock AddCallback
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	poller, err := NewProbePoller(config, baseLogger, addDataPoint)
	if err == nil {
		t.Error("Expected error when creating probe poller with invalid type")
	}
	if poller != nil {
		t.Error("Expected nil poller when creation fails")
	}
}

func TestGenerateProbeId_UniqueForDifferentConfigs(t *testing.T) {
	// Generate IDs for multiple different configs and ensure they're all unique
	configs := []configuration.ProbeConfig{
		{Name: "cpu-1", Type: "cpu", Params: map[string]interface{}{"interval": 30}},
		{Name: "cpu-2", Type: "cpu", Params: map[string]interface{}{"interval": 60}},
		{Name: "memory-1", Type: "memory", Params: map[string]interface{}{"interval": 30}},
		{Name: "citrix-prod", Type: "citrix", Params: map[string]interface{}{"base_url": "https://prod.example.com"}},
		{Name: "citrix-dev", Type: "citrix", Params: map[string]interface{}{"base_url": "https://dev.example.com"}},
	}

	ids := make(map[string]bool)
	for i, config := range configs {
		id := GenerateProbeId(config)
		if ids[id] {
			t.Errorf("Duplicate ID generated for config %d: %s", i, id)
		}
		ids[id] = true
	}

	// Verify we have the expected number of unique IDs
	if len(ids) != len(configs) {
		t.Errorf("Expected %d unique IDs, got %d", len(configs), len(ids))
	}
}

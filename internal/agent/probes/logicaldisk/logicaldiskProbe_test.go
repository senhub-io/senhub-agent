package logicaldisk

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// mockLogicalDiskCollector implements logicaldiskCollector for testing
type mockLogicalDiskCollector struct {
	collectData  []data_store.DataPoint
	collectError error
	closeError   error
	collectCount int
}

func (m *mockLogicalDiskCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	m.collectCount++
	if m.collectError != nil {
		return nil, m.collectError
	}
	return m.collectData, nil
}

func (m *mockLogicalDiskCollector) Close() error {
	return m.closeError
}

func TestNewLogicalDiskProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name        string
		config      map[string]interface{}
		wantErr     bool
		wantName    string
		description string
	}{
		{
			name:        "Valid probe with defaults",
			config:      map[string]interface{}{},
			wantErr:     false,
			wantName:    "logicaldisk",
			description: "Should create probe with default interval (30s)",
		},
		{
			name: "Valid probe with custom interval",
			config: map[string]interface{}{
				"interval": 60,
			},
			wantErr:     false,
			wantName:    "logicaldisk",
			description: "Should create probe with custom interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewLogicalDiskProbe(tt.config, baseLogger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogicalDiskProbe() error = %v, wantErr %v - %s", err, tt.wantErr, tt.description)
				return
			}

			if !tt.wantErr {
				// Test BaseProbe inheritance: SetName() and GetName()
				probe.(interface{ SetName(string) }).SetName(tt.wantName)
				if probe.GetName() != tt.wantName {
					t.Errorf("Expected name '%s', got '%s'", tt.wantName, probe.GetName())
				}

				// Verify default interval
				if tt.config["interval"] == nil {
					expectedInterval := 30 * time.Second
					if probe.GetInterval() != expectedInterval {
						t.Errorf("Expected default interval %v, got %v", expectedInterval, probe.GetInterval())
					}
				}
			}
		})
	}
}

func TestLogicalDiskProbe_GetName(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	// Test BaseProbe inheritance: SetName() and GetName()
	expected := "logicaldisk"
	probe.(interface{ SetName(string) }).SetName(expected)
	name := probe.GetName()
	if name != expected {
		t.Errorf("GetName() = %s, want %s", name, expected)
	}

	// Test default behavior: GetName() returns empty string before SetName() is called
	probe2, _ := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if probe2.GetName() != "" {
		t.Errorf("GetName() before SetName() = %s, want empty string", probe2.GetName())
	}
}

func TestLogicalDiskProbe_GetInterval(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name             string
		config           map[string]interface{}
		expectedInterval time.Duration
		description      string
	}{
		{
			name:             "Default interval",
			config:           map[string]interface{}{},
			expectedInterval: 30 * time.Second,
			description:      "Should use 30 seconds as default",
		},
		{
			name: "Custom interval 60 seconds",
			config: map[string]interface{}{
				"interval": 60,
			},
			expectedInterval: 60 * time.Second,
			description:      "Should use custom interval from config",
		},
		{
			name: "Custom interval 120 seconds",
			config: map[string]interface{}{
				"interval": 120,
			},
			expectedInterval: 120 * time.Second,
			description:      "Should use custom interval from config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewLogicalDiskProbe(tt.config, baseLogger)
			if err != nil {
				t.Fatalf("Failed to create probe: %v", err)
			}

			interval := probe.GetInterval()
			if interval != tt.expectedInterval {
				t.Errorf("GetInterval() = %v, want %v - %s", interval, tt.expectedInterval, tt.description)
			}
		})
	}
}

func TestLogicalDiskProbe_GetTargetStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	ldProbe := probe.(*logicaldiskProbe)
	strategies := ldProbe.GetTargetStrategies()
	expected := []string{"senhub", "prtg", "http", "otlp"}

	if len(strategies) != len(expected) {
		t.Errorf("GetTargetStrategies() returned %d strategies, want %d", len(strategies), len(expected))
	}

	for i, strategy := range strategies {
		if strategy != expected[i] {
			t.Errorf("GetTargetStrategies()[%d] = %s, want %s", i, strategy, expected[i])
		}
	}
}

func TestLogicalDiskProbe_ShouldStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	if !probe.ShouldStart() {
		t.Error("ShouldStart() should always return true")
	}
}

func TestLogicalDiskProbe_Collect(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	// Create probe and inject mock collector
	probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	// Set probe name for proper enrichment testing
	probe.(interface{ SetName(string) }).SetName("logicaldisk")

	ldProbe := probe.(*logicaldiskProbe)

	tests := []struct {
		name          string
		mockCollector *mockLogicalDiskCollector
		wantErr       bool
		wantMetrics   int
		description   string
	}{
		{
			name: "Successful collection",
			mockCollector: &mockLogicalDiskCollector{
				collectData: []data_store.DataPoint{
					{
						Name:      "logicaldisk.C.usage",
						Value:     75.5,
						Timestamp: time.Now(),
						Tags:      []tags.Tag{{Key: "disk", Value: "C"}},
					},
					{
						Name:      "logicaldisk.D.usage",
						Value:     45.2,
						Timestamp: time.Now(),
						Tags:      []tags.Tag{{Key: "disk", Value: "D"}},
					},
				},
				collectError: nil,
			},
			wantErr:     false,
			wantMetrics: 2,
			description: "Should collect metrics successfully",
		},
		{
			name: "Collection error",
			mockCollector: &mockLogicalDiskCollector{
				collectError: errors.New("failed to read disk stats"),
			},
			wantErr:     true,
			wantMetrics: 0,
			description: "Should return error when collection fails",
		},
		{
			name: "Empty metrics",
			mockCollector: &mockLogicalDiskCollector{
				collectData:  []data_store.DataPoint{},
				collectError: nil,
			},
			wantErr:     false,
			wantMetrics: 0,
			description: "Should handle empty metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inject mock collector
			ldProbe.collector = tt.mockCollector

			metrics, err := probe.Collect()
			if (err != nil) != tt.wantErr {
				t.Errorf("Collect() error = %v, wantErr %v - %s", err, tt.wantErr, tt.description)
				return
			}

			if !tt.wantErr {
				if len(metrics) != tt.wantMetrics {
					t.Errorf("Collect() returned %d metrics, want %d", len(metrics), tt.wantMetrics)
				}

				// Verify metrics were enriched with probe name
				for _, metric := range metrics {
					hasProbeTag := false
					for _, tag := range metric.Tags {
						if tag.Key == "probe_name" && tag.Value == "logicaldisk" {
							hasProbeTag = true
							break
						}
					}
					if !hasProbeTag {
						t.Error("Metric not enriched with probe_name tag")
					}
				}
			}

			// Verify collector was called
			if tt.mockCollector.collectCount != 1 {
				t.Errorf("Expected collector.Collect() called once, got %d calls", tt.mockCollector.collectCount)
			}
		})
	}
}

func TestLogicalDiskProbe_OnStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	quitChannel := make(chan struct{})
	err = probe.OnStart(quitChannel)
	if err != nil {
		t.Errorf("OnStart() should not return error, got: %v", err)
	}
}

func TestLogicalDiskProbe_OnShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name          string
		mockCollector *mockLogicalDiskCollector
		wantErr       bool
		description   string
	}{
		{
			name: "Successful shutdown",
			mockCollector: &mockLogicalDiskCollector{
				closeError: nil,
			},
			wantErr:     false,
			description: "Should shutdown without error",
		},
		{
			name: "Shutdown with error",
			mockCollector: &mockLogicalDiskCollector{
				closeError: errors.New("failed to close collector"),
			},
			wantErr:     true,
			description: "Should return error from collector.Close()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
			if err != nil {
				t.Fatalf("Failed to create probe: %v", err)
			}

			ldProbe := probe.(*logicaldiskProbe)
			ldProbe.collector = tt.mockCollector

			ctx := context.Background()
			err = probe.OnShutdown(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("OnShutdown() error = %v, wantErr %v - %s", err, tt.wantErr, tt.description)
			}
		})
	}
}

func TestLogicalDiskProbe_IsHealthy(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name          string
		mockCollector *mockLogicalDiskCollector
		wantHealthy   bool
		description   string
	}{
		{
			name: "Healthy probe",
			mockCollector: &mockLogicalDiskCollector{
				collectData: []data_store.DataPoint{
					{
						Name:      "logicaldisk.C.usage",
						Value:     75.5,
						Timestamp: time.Now(),
					},
				},
				collectError: nil,
			},
			wantHealthy: true,
			description: "Should be healthy when collection succeeds",
		},
		{
			name: "Unhealthy probe",
			mockCollector: &mockLogicalDiskCollector{
				collectError: errors.New("collection failed"),
			},
			wantHealthy: false,
			description: "Should be unhealthy when collection fails",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewLogicalDiskProbe(map[string]interface{}{}, baseLogger)
			if err != nil {
				t.Fatalf("Failed to create probe: %v", err)
			}

			ldProbe := probe.(*logicaldiskProbe)
			ldProbe.collector = tt.mockCollector

			healthy := ldProbe.IsHealthy()
			if healthy != tt.wantHealthy {
				t.Errorf("IsHealthy() = %v, want %v - %s", healthy, tt.wantHealthy, tt.description)
			}
		})
	}
}

func TestLogicalDiskProbe_String(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name        string
		config      map[string]interface{}
		wantContain []string
		description string
	}{
		{
			name:   "Default interval",
			config: map[string]interface{}{},
			wantContain: []string{
				"logicaldiskProbe",
				"name=logicaldisk",
				"30s",
			},
			description: "Should contain probe details with default interval",
		},
		{
			name: "Custom interval",
			config: map[string]interface{}{
				"interval": 60,
			},
			wantContain: []string{
				"logicaldiskProbe",
				"name=logicaldisk",
				"1m",
			},
			description: "Should contain probe details with custom interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewLogicalDiskProbe(tt.config, baseLogger)
			if err != nil {
				t.Fatalf("Failed to create probe: %v", err)
			}

			// Set probe name for String() testing
			probe.(interface{ SetName(string) }).SetName("logicaldisk")

			ldProbe := probe.(*logicaldiskProbe)
			str := ldProbe.String()
			for _, expected := range tt.wantContain {
				if !contains(str, expected) {
					t.Errorf("String() = %s, should contain '%s' - %s", str, expected, tt.description)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

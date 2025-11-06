package cpu

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

// mockOSCollector implements osCollector for testing
type mockOSCollector struct {
	collectData  []data_store.DataPoint
	collectError error
	closeError   error
	collectCount int
}

func (m *mockOSCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	m.collectCount++
	if m.collectError != nil {
		return nil, m.collectError
	}
	return m.collectData, nil
}

func (m *mockOSCollector) Close() error {
	return m.closeError
}

func TestNewCpuProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name        string
		config      map[string]interface{}
		wantErr     bool
		description string
	}{
		{
			name:        "Valid probe with defaults",
			config:      map[string]interface{}{},
			wantErr:     false,
			description: "Should create probe with default interval (30s)",
		},
		{
			name: "Valid probe with custom interval",
			config: map[string]interface{}{
				"interval": 60,
			},
			wantErr:     false,
			description: "Should create probe with custom interval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewCpuProbe(tt.config, baseLogger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCpuProbe() error = %v, wantErr %v - %s", err, tt.wantErr, tt.description)
				return
			}

			if !tt.wantErr {
				// Test BaseProbe inheritance: SetName() and GetName()
				probe.(interface{ SetName(string) }).SetName("cpu")
				if probe.GetName() != "cpu" {
					t.Errorf("Expected name 'cpu', got '%s'", probe.GetName())
				}
			}
		})
	}
}

func TestCpuProbe_GetName(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewCpuProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}

	// Test BaseProbe inheritance: SetName() and GetName()
	// In production, probe_poller.go calls SetName() after probe creation
	probe.(interface{ SetName(string) }).SetName("cpu")
	if probe.GetName() != "cpu" {
		t.Errorf("GetName() = %s, want 'cpu'", probe.GetName())
	}

	// Test default behavior: GetName() returns empty string before SetName() is called
	probe2, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	if probe2.GetName() != "" {
		t.Errorf("GetName() before SetName() = %s, want empty string", probe2.GetName())
	}
}

func TestCpuProbe_GetInterval(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name             string
		config           map[string]interface{}
		expectedInterval time.Duration
	}{
		{
			name:             "Default interval",
			config:           map[string]interface{}{},
			expectedInterval: 30 * time.Second,
		},
		{
			name:             "Custom interval",
			config:           map[string]interface{}{"interval": 60},
			expectedInterval: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, _ := NewCpuProbe(tt.config, baseLogger)
			if probe.GetInterval() != tt.expectedInterval {
				t.Errorf("GetInterval() = %v, want %v", probe.GetInterval(), tt.expectedInterval)
			}
		})
	}
}

func TestCpuProbe_GetTargetStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	cpuProbe := probe.(*cpuProbe)
	strategies := cpuProbe.GetTargetStrategies()
	expected := []string{"senhub", "prtg", "http"}

	if len(strategies) != len(expected) {
		t.Errorf("GetTargetStrategies() returned %d strategies, want %d", len(strategies), len(expected))
	}
}

func TestCpuProbe_ShouldStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	if !probe.ShouldStart() {
		t.Error("ShouldStart() should return true")
	}
}

func TestCpuProbe_Collect(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	cpuProbe := probe.(*cpuProbe)

	tests := []struct {
		name          string
		mockCollector *mockOSCollector
		wantErr       bool
		wantMetrics   int
	}{
		{
			name: "Successful collection",
			mockCollector: &mockOSCollector{
				collectData: []data_store.DataPoint{
					{Name: "cpu.usage", Value: 45.5, Timestamp: time.Now(), Tags: []tags.Tag{{Key: "core", Value: "0"}}},
					{Name: "cpu.usage", Value: 52.3, Timestamp: time.Now(), Tags: []tags.Tag{{Key: "core", Value: "1"}}},
				},
			},
			wantErr:     false,
			wantMetrics: 2,
		},
		{
			name:          "Collection error",
			mockCollector: &mockOSCollector{collectError: errors.New("failed to read CPU stats")},
			wantErr:       true,
			wantMetrics:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpuProbe.collector = tt.mockCollector

			metrics, err := probe.Collect()
			if (err != nil) != tt.wantErr {
				t.Errorf("Collect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(metrics) != tt.wantMetrics {
				t.Errorf("Collect() returned %d metrics, want %d", len(metrics), tt.wantMetrics)
			}
		})
	}
}

func TestCpuProbe_OnStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	quitChannel := make(chan struct{})

	if err := probe.OnStart(quitChannel); err != nil {
		t.Errorf("OnStart() should not return error, got: %v", err)
	}
}

func TestCpuProbe_OnShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name          string
		mockCollector *mockOSCollector
		wantErr       bool
	}{
		{
			name:          "Successful shutdown",
			mockCollector: &mockOSCollector{closeError: nil},
			wantErr:       false,
		},
		{
			name:          "Shutdown with error",
			mockCollector: &mockOSCollector{closeError: errors.New("close failed")},
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
			cpuProbe := probe.(*cpuProbe)
			cpuProbe.collector = tt.mockCollector

			ctx := context.Background()
			err := probe.OnShutdown(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("OnShutdown() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCpuProbe_IsHealthy(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	cpuProbe := probe.(*cpuProbe)

	cpuProbe.collector = &mockOSCollector{
		collectData: []data_store.DataPoint{{Name: "cpu.usage", Value: 50.0, Timestamp: time.Now()}},
	}

	if !cpuProbe.IsHealthy() {
		t.Error("IsHealthy() should return true when collection succeeds")
	}
}

func TestCpuProbe_String(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewCpuProbe(map[string]interface{}{}, baseLogger)
	cpuProbe := probe.(*cpuProbe)

	str := cpuProbe.String()
	if str == "" {
		t.Error("String() should not return empty string")
	}
}

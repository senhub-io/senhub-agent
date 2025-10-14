package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type mockOSCollector struct {
	collectData  []data_store.DataPoint
	collectError error
	closeError   error
}

func (m *mockOSCollector) Collect(timestamp time.Time) ([]data_store.DataPoint, error) {
	if m.collectError != nil {
		return nil, m.collectError
	}
	return m.collectData, nil
}

func (m *mockOSCollector) Close() error {
	return m.closeError
}

func TestNewMemoryProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{"Valid probe", map[string]interface{}{}, false},
		{"Custom interval", map[string]interface{}{"interval": 60}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewMemoryProbe(tt.config, baseLogger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMemoryProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && probe.GetName() != "memory" {
				t.Errorf("Expected name 'memory', got '%s'", probe.GetName())
			}
		})
	}
}

func TestMemoryProbe_GetInterval(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewMemoryProbe(map[string]interface{}{}, baseLogger)
	if probe.GetInterval() != 30*time.Second {
		t.Errorf("GetInterval() = %v, want 30s", probe.GetInterval())
	}
}

func TestMemoryProbe_GetTargetStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewMemoryProbe(map[string]interface{}{}, baseLogger)
	memProbe := probe.(*memoryProbe)
	strategies := memProbe.GetTargetStrategies()

	if len(strategies) != 3 {
		t.Errorf("GetTargetStrategies() returned %d, want 3", len(strategies))
	}
}

func TestMemoryProbe_Collect(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewMemoryProbe(map[string]interface{}{}, baseLogger)
	memProbe := probe.(*memoryProbe)

	tests := []struct {
		name          string
		mockCollector *mockOSCollector
		wantErr       bool
	}{
		{"Success", &mockOSCollector{collectData: []data_store.DataPoint{{Name: "memory.usage", Value: 8.5, Timestamp: time.Now()}}}, false},
		{"Error", &mockOSCollector{collectError: errors.New("fail")}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memProbe.collector = tt.mockCollector
			_, err := probe.Collect()
			if (err != nil) != tt.wantErr {
				t.Errorf("Collect() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryProbe_OnShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewMemoryProbe(map[string]interface{}{}, baseLogger)
	memProbe := probe.(*memoryProbe)
	memProbe.collector = &mockOSCollector{closeError: nil}

	ctx := context.Background()
	if err := probe.OnShutdown(ctx); err != nil {
		t.Errorf("OnShutdown() error = %v", err)
	}
}

func TestMemoryProbe_IsHealthy(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewMemoryProbe(map[string]interface{}{}, baseLogger)
	memProbe := probe.(*memoryProbe)
	memProbe.collector = &mockOSCollector{collectData: []data_store.DataPoint{{Name: "memory.usage", Value: 8.0, Timestamp: time.Now()}}}

	if !memProbe.IsHealthy() {
		t.Error("IsHealthy() should return true")
	}
}

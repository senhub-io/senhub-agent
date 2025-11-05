package network

import (
	"context"
	"errors"
	"runtime"
	"strings"
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

func TestNewNetworkProbe(t *testing.T) {
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
			probe, err := NewNetworkProbe(tt.config, baseLogger)

			// On Windows, PDH counters may not be available in test environments
			if runtime.GOOS == "windows" && err != nil {
				if strings.Contains(err.Error(), "PDH_NO_DATA") ||
					strings.Contains(err.Error(), "failed initial collection") {
					t.Logf("Expected Windows PDH limitation: %v", err)
					return
				}
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewNetworkProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				// Test BaseProbe inheritance: SetName() and GetName()
				probe.(interface{ SetName(string) }).SetName("network")
				if probe.GetName() != "network" {
					t.Errorf("Expected name 'network', got '%s'", probe.GetName())
				}
			}
		})
	}
}

func TestNetworkProbe_GetInterval(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewNetworkProbe(map[string]interface{}{}, baseLogger)
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "PDH_NO_DATA") {
		t.Skip("Skipping on Windows PDH limitation")
	}
	if err == nil && probe.GetInterval() != 30*time.Second {
		t.Errorf("GetInterval() = %v, want 30s", probe.GetInterval())
	}
}

func TestNetworkProbe_GetTargetStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewNetworkProbe(map[string]interface{}{}, baseLogger)
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "PDH_NO_DATA") {
		t.Skip("Skipping on Windows PDH limitation")
	}
	if err == nil {
		netProbe := probe.(*networkProbe)
		strategies := netProbe.GetTargetStrategies()
		if len(strategies) != 3 {
			t.Errorf("GetTargetStrategies() returned %d, want 3", len(strategies))
		}
	}
}

func TestNetworkProbe_Collect(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewNetworkProbe(map[string]interface{}{}, baseLogger)
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "PDH_NO_DATA") {
		t.Skip("Skipping on Windows PDH limitation")
	}
	if err == nil {
		netProbe := probe.(*networkProbe)

		tests := []struct {
			name          string
			mockCollector *mockOSCollector
			wantErr       bool
		}{
			{"Success", &mockOSCollector{collectData: []data_store.DataPoint{{Name: "network.bytes_sent", Value: 1024.0, Timestamp: time.Now()}}}, false},
			{"Error", &mockOSCollector{collectError: errors.New("fail")}, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				netProbe.collector = tt.mockCollector
				_, err := probe.Collect()
				if (err != nil) != tt.wantErr {
					t.Errorf("Collect() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	}
}

func TestNetworkProbe_OnShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewNetworkProbe(map[string]interface{}{}, baseLogger)
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "PDH_NO_DATA") {
		t.Skip("Skipping on Windows PDH limitation")
	}
	if err == nil {
		netProbe := probe.(*networkProbe)
		netProbe.collector = &mockOSCollector{closeError: nil}

		ctx := context.Background()
		if err := probe.OnShutdown(ctx); err != nil {
			t.Errorf("OnShutdown() error = %v", err)
		}
	}
}

func TestNetworkProbe_IsHealthy(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, err := NewNetworkProbe(map[string]interface{}{}, baseLogger)
	if runtime.GOOS == "windows" && err != nil && strings.Contains(err.Error(), "PDH_NO_DATA") {
		t.Skip("Skipping on Windows PDH limitation")
	}
	if err == nil {
		netProbe := probe.(*networkProbe)
		netProbe.collector = &mockOSCollector{collectData: []data_store.DataPoint{{Name: "network.bytes_sent", Value: 1024.0, Timestamp: time.Now()}}}

		if !netProbe.IsHealthy() {
			t.Error("IsHealthy() should return true")
		}
	}
}

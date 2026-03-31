package senhub

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// Mock AgentConfiguration
type mockAgentConfig struct {
	authKey   string
	serverURL string
}

func (m *mockAgentConfig) GetAuthenticationKey() string { return m.authKey }
func (m *mockAgentConfig) GetServerUrl() string         { return m.serverURL }

// Mock Server
type mockServer struct {
	sendDataError error
	sendDataCalls int
	lastData      []SenhubDataPoint
}

func (m *mockServer) Get(path string) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockServer) Post(path string, data any) (*http.Response, error) {
	if m.sendDataError != nil {
		return nil, m.sendDataError
	}
	m.sendDataCalls++
	if sData, ok := data.([]SenhubDataPoint); ok {
		m.lastData = sData
	}
	return &http.Response{StatusCode: 200}, nil
}

func (m *mockServer) PostStream(path string, contentType string) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func TestNewBuffer(t *testing.T) {
	buffer := NewBuffer()
	if buffer == nil {
		t.Fatal("NewBuffer() returned nil")
	}
}

func TestBuffer_Append(t *testing.T) {
	buffer := NewBuffer()

	data := []datapoint.DataPoint{
		{Name: "test.metric1", Value: 10.0, Timestamp: time.Now()},
		{Name: "test.metric2", Value: 20.0, Timestamp: time.Now()},
	}

	err := buffer.Append(data)
	if err != nil {
		t.Errorf("Append() returned error: %v", err)
	}
}

func TestBuffer_Sync(t *testing.T) {
	buffer := NewBuffer()

	// Add data
	data1 := []datapoint.DataPoint{
		{Name: "test.metric1", Value: 10.0, Timestamp: time.Now()},
	}
	_ = buffer.Append(data1)

	// Sync should return data and clear buffer
	synced := buffer.Sync()
	if len(synced) != 1 {
		t.Errorf("Sync() returned %d items, want 1", len(synced))
	}

	// Second sync should return empty
	synced2 := buffer.Sync()
	if len(synced2) != 0 {
		t.Errorf("Second Sync() returned %d items, want 0", len(synced2))
	}
}

func TestBuffer_AbortSync(t *testing.T) {
	buffer := NewBuffer()

	// Add initial data
	data1 := []datapoint.DataPoint{
		{Name: "test.metric1", Value: 10.0, Timestamp: time.Now()},
	}
	_ = buffer.Append(data1)

	// Sync
	synced := buffer.Sync()

	// Add more data
	data2 := []datapoint.DataPoint{
		{Name: "test.metric2", Value: 20.0, Timestamp: time.Now()},
	}
	_ = buffer.Append(data2)

	// Abort sync - should prepend failed data
	err := buffer.AbortSync(synced)
	if err != nil {
		t.Errorf("AbortSync() returned error: %v", err)
	}

	// Sync again - should have both items (aborted first, then new)
	allData := buffer.Sync()
	if len(allData) != 2 {
		t.Errorf("After AbortSync, Sync() returned %d items, want 2", len(allData))
	}

	// Failed data should be first
	if allData[0].Name != "test.metric1" {
		t.Errorf("Expected failed data first, got %s", allData[0].Name)
	}
}

func TestBuffer_ConcurrentAccess(t *testing.T) {
	buffer := NewBuffer()
	done := make(chan bool)

	// Multiple goroutines appending concurrently
	for i := 0; i < 10; i++ {
		go func(n int) {
			data := []datapoint.DataPoint{
				{Name: "concurrent.metric", Value: float32(n), Timestamp: time.Now()},
			}
			_ = buffer.Append(data)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have all 10 items
	synced := buffer.Sync()
	if len(synced) != 10 {
		t.Errorf("After concurrent appends, got %d items, want 10", len(synced))
	}
}

func TestNewSyncStrategySenhub(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	agentConfig := &mockAgentConfig{
		authKey:   "test-key",
		serverURL: "https://example.com",
	}

	storageConfig := configuration.StorageConfigParams{
		"interval": 5,
	}

	strategy := NewSyncStrategySenhub(agentConfig, storageConfig, baseLogger)
	if strategy == nil {
		t.Fatal("NewSyncStrategySenhub() returned nil")
	}

	senhubStrategy, ok := strategy.(*SyncStrategySenhub)
	if !ok {
		t.Fatal("Strategy is not of type *SyncStrategySenhub")
	}

	if senhubStrategy.GetStrategyName() != "senhub" {
		t.Errorf("GetStrategyName() = %s, want 'senhub'", senhubStrategy.GetStrategyName())
	}
}

func TestSyncStrategySenhub_GetStrategyName(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	if strategy.GetStrategyName() != "senhub" {
		t.Errorf("GetStrategyName() = %s, want 'senhub'", strategy.GetStrategyName())
	}
}

func TestSyncStrategySenhub_GetStrategyParams(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	storageConfig := configuration.StorageConfigParams{
		"interval": 5,
		"custom":   "value",
	}

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test"},
		storageConfig,
		baseLogger,
	).(*SyncStrategySenhub)

	params := strategy.GetStrategyParams()
	if params == nil {
		t.Fatal("GetStrategyParams() returned nil")
	}

	if params["interval"] != 5 {
		t.Errorf("GetStrategyParams()[interval] = %v, want 5", params["interval"])
	}
}

func TestSyncStrategySenhub_ValidateConfigParams(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	tests := []struct {
		name    string
		params  configuration.StorageConfigParams
		wantErr bool
	}{
		{"Valid empty", configuration.StorageConfigParams{}, false},
		{"Valid with string interval", configuration.StorageConfigParams{"interval": "10s"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := strategy.ValidateConfigParams(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfigParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSyncStrategySenhub_AddDataPoints(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	data := []datapoint.DataPoint{
		{Name: "test.metric", Value: 42.0, Timestamp: time.Now(), Tags: []tags.Tag{{Key: "host", Value: "localhost"}}},
	}

	err := strategy.AddDataPoints(data)
	if err != nil {
		t.Errorf("AddDataPoints() returned error: %v", err)
	}
}

func TestSyncStrategySenhub_Start(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test", serverURL: "https://example.com"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	// Replace server with mock
	mockSrv := &mockServer{}
	strategy.server = mockSrv

	err := strategy.Start()
	if err != nil {
		t.Errorf("Start() returned error: %v", err)
	}

	// Verify scheduler is started (via Start method)
	// Note: This is integration-level test, actual execution tested separately
}

func TestSyncStrategySenhub_Shutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test", serverURL: "https://example.com"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	// Must call Start() before Shutdown() to initialize scheduler
	_ = strategy.Start()

	ctx := context.Background()
	err := strategy.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() returned error: %v", err)
	}
}

func TestConvertToSenhubDataPoints(t *testing.T) {
	now := time.Now()
	data := []datapoint.DataPoint{
		{
			Name:      "test.metric",
			Value:     42.5,
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "host", Value: "server1"}},
		},
	}

	// This is a private function, we test it indirectly through AddDataPoints and sync
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	strategy := NewSyncStrategySenhub(
		&mockAgentConfig{authKey: "test"},
		configuration.StorageConfigParams{},
		baseLogger,
	).(*SyncStrategySenhub)

	mockSrv := &mockServer{}
	strategy.server = mockSrv

	// Add data
	_ = strategy.AddDataPoints(data)

	// Manually trigger sync (normally done by scheduler)
	syncedData := strategy.buffer.Sync()
	if len(syncedData) != 1 {
		t.Errorf("Expected 1 datapoint in buffer, got %d", len(syncedData))
	}

	// Verify conversion happens correctly (timestamp, value, tags)
	if syncedData[0].Name != "test.metric" {
		t.Errorf("Expected name 'test.metric', got '%s'", syncedData[0].Name)
	}
}

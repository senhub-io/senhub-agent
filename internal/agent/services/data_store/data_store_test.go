package data_store

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// MockConfigProvider implements configuration.ConfigurationProvider for testing
type MockConfigProvider struct {
	config configuration.ConfigurationData
}

func (m *MockConfigProvider) GetConfiguration() configuration.ConfigurationData {
	return m.config
}

func (m *MockConfigProvider) OnConfigChanged(callback func(string)) {}
func (m *MockConfigProvider) GetName() string                       { return "MockConfigProvider" }
func (m *MockConfigProvider) Start(chan struct{}) error             { return nil }
func (m *MockConfigProvider) Shutdown(context.Context) error        { return nil }

// MockAgentConfig implements configuration.AgentConfiguration for testing
type MockAgentConfig struct {
	authKey   string
	serverURL string
}

func (m *MockAgentConfig) GetAuthenticationKey() string     { return m.authKey }
func (m *MockAgentConfig) GetGlobalTags() map[string]string { return nil }

// MockStrategy implements SyncStrategy for testing
// MockStrategy records lifecycle and datapoint calls. It is mutex-
// protected: production strategies synchronize internally, and tests
// like the hot-reload regression hammer the same instance from several
// callback goroutines — an unsynchronized mock made the race job
// flaky on a race that only existed in the test double.
type MockStrategy struct {
	mu            sync.Mutex
	name          string
	params        map[string]interface{}
	dataPoints    [][]datapoint.DataPoint
	started       bool
	shutdown      bool
	validateError error
	startError    error
	addError      error
}

func (m *MockStrategy) GetStrategyName() string                   { return m.name }
func (m *MockStrategy) GetStrategyParams() map[string]interface{} { return m.params }
func (m *MockStrategy) ValidateConfigParams(configuration.StorageConfigParams) error {
	return m.validateError
}
func (m *MockStrategy) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return m.startError
}
func (m *MockStrategy) AddDataPoints(data []datapoint.DataPoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dataPoints = append(m.dataPoints, data)
	return m.addError
}
func (m *MockStrategy) Shutdown(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdown = true
	return nil
}

func (m *MockStrategy) wasShutdown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdown
}

// MockStrategyRouter implements StrategyRouter for testing
type MockStrategyRouter struct {
	targets []string
}

func (m *MockStrategyRouter) GetTargetStrategies() []string {
	return m.targets
}

func TestNewDataStore(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{authKey: "test-key", serverURL: "https://example.com"}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger)

	if ds == nil {
		t.Fatal("NewDataStore returned nil")
	}

	if ds.GetName() != "DataStore" {
		t.Errorf("Expected name 'DataStore', got '%s'", ds.GetName())
	}
}

func TestGenerateStrategyId(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{authKey: "test-key", serverURL: "https://example.com"}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	tests := []struct {
		name         string
		strategyName string
		params1      configuration.StorageConfigParams
		params2      configuration.StorageConfigParams
		shouldMatch  bool
		description  string
	}{
		{
			name:         "Identical configs produce same ID",
			strategyName: "senhub",
			params1:      map[string]interface{}{"url": "https://example.com"},
			params2:      map[string]interface{}{"url": "https://example.com"},
			shouldMatch:  true,
			description:  "Same parameters should generate same ID",
		},
		{
			name:         "Different params produce different IDs",
			strategyName: "senhub",
			params1:      map[string]interface{}{"url": "https://example.com"},
			params2:      map[string]interface{}{"url": "https://different.com"},
			shouldMatch:  false,
			description:  "Different parameters should generate different IDs",
		},
		{
			name:         "Empty params are deterministic",
			strategyName: "prtg",
			params1:      map[string]interface{}{},
			params2:      map[string]interface{}{},
			shouldMatch:  true,
			description:  "Empty params should generate same ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := ds.GenerateStrategyId(tt.strategyName, tt.params1)
			id2 := ds.GenerateStrategyId(tt.strategyName, tt.params2)

			// IDs should be 64 chars (SHA256 hex)
			if len(id1) != 64 {
				t.Errorf("ID1 should be 64 characters, got %d", len(id1))
			}
			if len(id2) != 64 {
				t.Errorf("ID2 should be 64 characters, got %d", len(id2))
			}

			if tt.shouldMatch {
				if id1 != id2 {
					t.Errorf("Expected matching IDs, got %s and %s", id1, id2)
				}
			} else {
				if id1 == id2 {
					t.Errorf("Expected different IDs, got same: %s", id1)
				}
			}
		})
	}
}

func TestConvertMapTypes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "String remains unchanged",
			input:    "test",
			expected: "test",
		},
		{
			name:     "Int remains unchanged",
			input:    123,
			expected: 123,
		},
		{
			name: "map[string]interface{} converted properly",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": 123,
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": 123,
			},
		},
		{
			name: "Nested map converted properly",
			input: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value",
				},
			},
			expected: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value",
				},
			},
		},
		{
			name:     "Array converted properly",
			input:    []interface{}{1, 2, 3},
			expected: []interface{}{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ds.convertMapTypes(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("convertMapTypes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCallback(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	// Add a mock strategy
	mockStrategy := &MockStrategy{
		name:   "test-strategy",
		params: map[string]interface{}{},
	}
	func() { v := []SyncStrategy{mockStrategy}; ds.strategies.Store(&v) }()

	callback := ds.GetCallback()
	if callback == nil {
		t.Fatal("GetCallback returned nil")
	}

	// Test callback with data
	testData := []datapoint.DataPoint{
		{
			Name:      "test.metric",
			Value:     100.0,
			Timestamp: time.Now(),
			Tags:      []tags.Tag{{Key: "test", Value: "value"}},
		},
	}

	router := &MockStrategyRouter{targets: []string{"test-strategy"}}
	err := callback(testData, router)
	if err != nil {
		t.Errorf("Callback returned error: %v", err)
	}

	// Verify data was sent to strategy
	if len(mockStrategy.dataPoints) != 1 {
		t.Errorf("Expected 1 call to AddDataPoints, got %d", len(mockStrategy.dataPoints))
	}

	if len(mockStrategy.dataPoints[0]) != 1 {
		t.Errorf("Expected 1 datapoint, got %d", len(mockStrategy.dataPoints[0]))
	}
}

func TestGetCallback_AppliesConfiguredTags(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockProvider := &MockConfigProvider{config: configuration.ConfigurationData{
		Agent: configuration.AgentConfig{GlobalTags: map[string]string{"site": "global-x", "region": "west"}},
		Probes: []configuration.ProbeConfig{
			{Name: "p1", CustomTags: map[string]string{"site": "custom-x", "tier": "gold"}},
		},
	}}
	ds := NewDataStore(&MockAgentConfig{}, mockProvider, baseLogger).(*dataStore)
	mockStrategy := &MockStrategy{name: "s", params: map[string]interface{}{}}
	func() { v := []SyncStrategy{mockStrategy}; ds.strategies.Store(&v) }()

	in := []datapoint.DataPoint{
		{Name: "m", Tags: []tags.Tag{{Key: "probe_name", Value: "p1"}, {Key: "site", Value: "builtin"}}},
		{Name: "m", Tags: []tags.Tag{{Key: "probe_name", Value: "p2"}}},
	}
	if err := ds.GetCallback()(in, &MockStrategyRouter{targets: []string{"s"}}); err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if len(mockStrategy.dataPoints) != 1 {
		t.Fatalf("want 1 AddDataPoints call, got %d", len(mockStrategy.dataPoints))
	}
	got := mockStrategy.dataPoints[0]
	val := func(tt []tags.Tag, k string) string {
		for _, x := range tt {
			if x.Key == k {
				return x.Value
			}
		}
		return ""
	}
	// p1: custom_tags > global_tags > built-in
	if v := val(got[0].Tags, "site"); v != "custom-x" {
		t.Errorf("p1 site = %q, want custom-x (custom wins)", v)
	}
	if v := val(got[0].Tags, "region"); v != "west" {
		t.Errorf("p1 region = %q, want west (global)", v)
	}
	if v := val(got[0].Tags, "tier"); v != "gold" {
		t.Errorf("p1 tier = %q, want gold (custom)", v)
	}
	// p2: no custom_tags → global wins over built-in (none here), applied to all probes
	if v := val(got[1].Tags, "site"); v != "global-x" {
		t.Errorf("p2 site = %q, want global-x (global applies to every probe)", v)
	}
	if v := val(got[1].Tags, "tier"); v != "" {
		t.Errorf("p2 tier = %q, want empty (custom_tags are per-probe)", v)
	}
}

func TestGetCallback_NoStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	callback := ds.GetCallback()

	testData := []datapoint.DataPoint{
		{Name: "test", Value: 1.0, Timestamp: time.Now()},
	}

	router := &MockStrategyRouter{targets: []string{"any-strategy"}}
	err := callback(testData, router)
	if err != nil {
		t.Errorf("Callback with no strategies should not error, got: %v", err)
	}
}

func TestGetCallback_StrategyFiltering(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	// Add multiple strategies
	strategy1 := &MockStrategy{name: "strategy1"}
	strategy2 := &MockStrategy{name: "strategy2"}
	func() { v := []SyncStrategy{strategy1, strategy2}; ds.strategies.Store(&v) }()

	callback := ds.GetCallback()

	testData := []datapoint.DataPoint{
		{Name: "test", Value: 1.0, Timestamp: time.Now()},
	}

	// Target only strategy1
	router := &MockStrategyRouter{targets: []string{"strategy1"}}
	err := callback(testData, router)
	if err != nil {
		t.Errorf("Callback returned error: %v", err)
	}

	// Verify only strategy1 received data
	if len(strategy1.dataPoints) != 1 {
		t.Errorf("Strategy1 should have received data, got %d calls", len(strategy1.dataPoints))
	}
	if len(strategy2.dataPoints) != 0 {
		t.Errorf("Strategy2 should not have received data, got %d calls", len(strategy2.dataPoints))
	}
}

func TestStartAndShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{},
		},
	}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger)

	quitChannel := make(chan struct{})
	err := ds.Start(quitChannel)
	if err != nil {
		t.Errorf("Start returned error: %v", err)
	}

	ctx := context.Background()
	err = ds.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}
}

func TestShutdown_WithStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	// Add mock strategies
	strategy1 := &MockStrategy{name: "strategy1"}
	strategy2 := &MockStrategy{name: "strategy2"}
	func() { v := []SyncStrategy{strategy1, strategy2}; ds.strategies.Store(&v) }()

	ctx := context.Background()
	err := ds.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown returned error: %v", err)
	}

	// Verify both strategies were shutdown
	if !strategy1.wasShutdown() {
		t.Error("Strategy1 was not shutdown")
	}
	if !strategy2.wasShutdown() {
		t.Error("Strategy2 was not shutdown")
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("getTagValue", func(t *testing.T) {
		tags := []tags.Tag{
			{Key: "key1", Value: "value1"},
			{Key: "key2", Value: "value2"},
		}

		if val := getTagValue(tags, "key1"); val != "value1" {
			t.Errorf("getTagValue('key1') = %s, want 'value1'", val)
		}

		if val := getTagValue(tags, "nonexistent"); val != "" {
			t.Errorf("getTagValue('nonexistent') = %s, want ''", val)
		}
	})

	t.Run("min", func(t *testing.T) {
		if min(5, 10) != 5 {
			t.Errorf("min(5, 10) = %d, want 5", min(5, 10))
		}
		if min(10, 5) != 5 {
			t.Errorf("min(10, 5) = %d, want 5", min(10, 5))
		}
		if min(5, 5) != 5 {
			t.Errorf("min(5, 5) = %d, want 5", min(5, 5))
		}
	})

	t.Run("truncateString", func(t *testing.T) {
		if truncateString("short", 10) != "short" {
			t.Error("Short string should not be truncated")
		}

		if truncateString("very long string", 10) != "very long ..." {
			t.Error("Long string should be truncated with ...")
		}

		if truncateString("exact", 5) != "exact" {
			t.Error("Exact length string should not be truncated")
		}
	})
}

func TestOnConfigRefreshed(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{authKey: "test-key"}
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{},
		},
	}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	// Test initial call
	ds.OnConfigRefreshed("test-reason")

	// Strategies should be empty with empty config
	if len(ds.activeStrategies()) != 0 {
		t.Errorf("Expected 0 strategies with empty config, got %d", len(ds.activeStrategies()))
	}
}

func TestGenerateStrategyId_Deterministic(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	params := map[string]interface{}{
		"port":    8080,
		"address": "127.0.0.1",
	}

	// Generate ID multiple times
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = ds.GenerateStrategyId("http", params)
	}

	// All IDs should be identical
	firstID := ids[0]
	for i, id := range ids {
		if id != firstID {
			t.Errorf("ID generation is not deterministic: iteration %d produced different ID", i)
		}
	}
}

func TestGenerateStrategyId_UniqueForDifferentConfigs(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockConfig := &MockAgentConfig{}
	mockProvider := &MockConfigProvider{}

	ds := NewDataStore(mockConfig, mockProvider, baseLogger).(*dataStore)

	configs := []struct {
		name   string
		params map[string]interface{}
	}{
		{"http", map[string]interface{}{"port": 8080}},
		{"http", map[string]interface{}{"port": 9090}},
		{"senhub", map[string]interface{}{"url": "https://example.com"}},
		{"prtg", map[string]interface{}{}},
	}

	ids := make(map[string]bool)
	for _, config := range configs {
		id := ds.GenerateStrategyId(config.name, config.params)
		if ids[id] {
			t.Errorf("Duplicate ID generated for config %s with params %v", config.name, config.params)
		}
		ids[id] = true
	}
}

func TestApplyUnitCorrections_InjectsUnitTagFromYAML(t *testing.T) {
	// The cpu probe sends "cpu_usage_total" with unit "%" declared in
	// the YAML but no "unit" tag on the datapoint. After applyUnitCorrections,
	// the datapoint must carry a "unit" tag = "%" so downstream consumers
	// (OTLP) can trigger the % → ratio conversion in otelmapper.Resolve.
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	ds := NewDataStore(&MockAgentConfig{}, &MockConfigProvider{}, baseLogger).(*dataStore)

	in := []datapoint.DataPoint{{
		Name:      "cpu_usage_total",
		Value:     27.9,
		Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu-local"},
			{Key: "probe_type", Value: "cpu"},
		},
	}}
	out := ds.applyUnitCorrections(in)
	if len(out) != 1 {
		t.Fatalf("got %d datapoints, want 1", len(out))
	}
	unitFound := ""
	for _, tag := range out[0].Tags {
		if tag.Key == "unit" {
			unitFound = tag.Value
		}
	}
	if unitFound != "%" {
		t.Errorf("unit tag = %q, want \"%%\" (sourced from cpu.yaml unit declaration)", unitFound)
	}
}

func TestApplyUnitCorrections_PreservesExistingUnitTag(t *testing.T) {
	// Probes that DO emit a unit tag (e.g. variable-unit probes) must
	// not be overridden by the YAML default. The injection only fires
	// when the tag is absent.
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	ds := NewDataStore(&MockAgentConfig{}, &MockConfigProvider{}, baseLogger).(*dataStore)

	in := []datapoint.DataPoint{{
		Name:      "cpu_usage_total",
		Value:     27.9,
		Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu-local"},
			{Key: "probe_type", Value: "cpu"},
			{Key: "unit", Value: "custom"},
		},
	}}
	out := ds.applyUnitCorrections(in)
	unitFound := ""
	unitCount := 0
	for _, tag := range out[0].Tags {
		if tag.Key == "unit" {
			unitFound = tag.Value
			unitCount++
		}
	}
	if unitCount != 1 || unitFound != "custom" {
		t.Errorf("unit tag should be preserved verbatim, got value=%q count=%d", unitFound, unitCount)
	}
}

func TestApplyUnitCorrections_NoProbeIdentitySkipsEnrichment(t *testing.T) {
	// Datapoints without probe identity (probe_name/type tags) are
	// passed through untouched — they can't be routed to a transformer.
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	ds := NewDataStore(&MockAgentConfig{}, &MockConfigProvider{}, baseLogger).(*dataStore)

	in := []datapoint.DataPoint{{
		Name:  "orphan_metric",
		Value: 42,
	}}
	out := ds.applyUnitCorrections(in)
	if len(out[0].Tags) != 0 {
		t.Errorf("orphan datapoint got tags injected: %+v", out[0].Tags)
	}
}

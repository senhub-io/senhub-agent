package sensor

import (
	"context"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// MockConfigProvider implements configuration.ConfigurationProvider for testing
type MockConfigProvider struct {
	config          configuration.ConfigurationData
	changeCallbacks []func(string)
}

func (m *MockConfigProvider) GetConfiguration() configuration.ConfigurationData {
	return m.config
}

func (m *MockConfigProvider) OnConfigChanged(callback func(string)) {
	m.changeCallbacks = append(m.changeCallbacks, callback)
}

func (m *MockConfigProvider) TriggerConfigChange(reason string) {
	for _, callback := range m.changeCallbacks {
		callback(reason)
	}
}

func (m *MockConfigProvider) GetName() string                { return "MockConfigProvider" }
func (m *MockConfigProvider) Start(chan struct{}) error      { return nil }
func (m *MockConfigProvider) Shutdown(context.Context) error { return nil }

func TestNewSensor(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)

	if sensor == nil {
		t.Fatal("NewSensor returned nil")
	}

	if sensor.GetName() != "Sensor" {
		t.Errorf("Expected name 'Sensor', got '%s'", sensor.GetName())
	}
}

func TestSensor_Start_NoProbes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start with no probes should not error, got: %v", err)
	}
}

func TestSensor_Start_WithValidProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "test-cpu",
					Type: "cpu",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start with valid probe should not error, got: %v", err)
	}

	// Cleanup
	ctx := context.Background()
	_ = sensor.Shutdown(ctx)
}

func TestSensor_Start_WithInvalidProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "invalid-probe",
					Type: "nonexistent_type",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	// Start should not fail even if probe creation fails
	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start should not error even with invalid probe, got: %v", err)
	}
}

func TestSensor_SyncConfiguration(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	s := NewSensor(addDataPoint, mockProvider, baseLogger).(*sensor)

	// Initial sync - no probes
	err := s.SyncConfiguration()
	if err != nil {
		t.Errorf("SyncConfiguration failed: %v", err)
	}

	// Add a probe
	mockProvider.config.Probes = append(mockProvider.config.Probes, configuration.ProbeConfig{
		Name: "test-memory",
		Type: "memory",
		Params: map[string]interface{}{
			"interval": 30,
		},
	})

	// Sync again - should start the probe
	err = s.SyncConfiguration()
	if err != nil {
		t.Errorf("SyncConfiguration after adding probe failed: %v", err)
	}

	// Verify probe was added
	if len(s.startedProbes) != 1 {
		t.Errorf("Expected 1 probe after sync, got %d", len(s.startedProbes))
	}

	// Cleanup
	ctx := context.Background()
	for _, probe := range s.startedProbes {
		_ = probe.Shutdown(ctx)
	}
}

func TestSensor_OnConfigChanged(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{},
		},
	}

	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	// Start sensor - registers config change callback
	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Add a probe and trigger config change
	mockProvider.config.Probes = append(mockProvider.config.Probes, configuration.ProbeConfig{
		Name: "test-cpu",
		Type: "cpu",
		Params: map[string]interface{}{
			"interval": 30,
		},
	})

	mockProvider.TriggerConfigChange("test-change")

	// Give callback time to execute
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	ctx := context.Background()
	_ = sensor.Shutdown(ctx)
}

func TestSensor_Shutdown_WithProbes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "test-cpu",
					Type: "cpu",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
				{
					Name: "test-memory",
					Type: "memory",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	// Start with multiple probes
	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}

	// Shutdown
	ctx := context.Background()
	err = sensor.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestSensor_Shutdown_NoProbes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)

	// Shutdown without starting
	ctx := context.Background()
	err := sensor.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown with no probes should not error, got: %v", err)
	}
}

func TestSensor_MultipleProbes_DifferentTypes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "cpu-monitor",
					Type: "cpu",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
				{
					Name: "memory-monitor",
					Type: "memory",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
				{
					Name: "network-monitor",
					Type: "network",
					Params: map[string]interface{}{
						"interval": 60,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	sensor := NewSensor(addDataPoint, mockProvider, baseLogger)
	quitChannel := make(chan struct{})

	err := sensor.Start(quitChannel)
	if err != nil {
		t.Errorf("Start with multiple probes failed: %v", err)
	}

	// Cleanup
	ctx := context.Background()
	_ = sensor.Shutdown(ctx)
}

func TestSensor_AddRemoveProbes(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "test-cpu",
					Type: "cpu",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	s := NewSensor(addDataPoint, mockProvider, baseLogger).(*sensor)

	// Initial sync with 1 probe
	err := s.SyncConfiguration()
	if err != nil {
		t.Errorf("Initial SyncConfiguration failed: %v", err)
	}

	if len(s.startedProbes) != 1 {
		t.Errorf("Expected 1 probe initially, got %d", len(s.startedProbes))
	}

	// Remove the probe
	mockProvider.config.Probes = []configuration.ProbeConfig{}

	err = s.SyncConfiguration()
	if err != nil {
		t.Errorf("SyncConfiguration after removing probe failed: %v", err)
	}

	if len(s.startedProbes) != 0 {
		t.Errorf("Expected 0 probes after removal, got %d", len(s.startedProbes))
	}

	// Add it back
	mockProvider.config.Probes = []configuration.ProbeConfig{
		{
			Name: "test-cpu",
			Type: "cpu",
			Params: map[string]interface{}{
				"interval": 30,
			},
		},
	}

	err = s.SyncConfiguration()
	if err != nil {
		t.Errorf("SyncConfiguration after re-adding probe failed: %v", err)
	}

	if len(s.startedProbes) != 1 {
		t.Errorf("Expected 1 probe after re-adding, got %d", len(s.startedProbes))
	}

	// Cleanup
	ctx := context.Background()
	for _, probe := range s.startedProbes {
		_ = probe.Shutdown(ctx)
	}
}

func TestSensor_DuplicateProbeNotStartedTwice(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{
				{
					Name: "test-cpu",
					Type: "cpu",
					Params: map[string]interface{}{
						"interval": 30,
					},
				},
			},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	s := NewSensor(addDataPoint, mockProvider, baseLogger).(*sensor)

	// First sync
	err := s.SyncConfiguration()
	if err != nil {
		t.Errorf("First SyncConfiguration failed: %v", err)
	}

	probeCountAfterFirst := len(s.startedProbes)

	// Second sync with same config - should not duplicate
	err = s.SyncConfiguration()
	if err != nil {
		t.Errorf("Second SyncConfiguration failed: %v", err)
	}

	if len(s.startedProbes) != probeCountAfterFirst {
		t.Errorf("Expected %d probes after second sync (no duplication), got %d", probeCountAfterFirst, len(s.startedProbes))
	}

	// Cleanup
	ctx := context.Background()
	for _, probe := range s.startedProbes {
		_ = probe.Shutdown(ctx)
	}
}

// TestSensor_SyncConfiguration_ConcurrentConfigEvents mimics the
// EventNotifier contract (each config event dispatched on a fresh
// goroutine) and fires two events back-to-back: SyncConfiguration must
// serialize them — no data race, no duplicated probes, final probe set
// consistent with the configuration.
func TestSensor_SyncConfiguration_ConcurrentConfigEvents(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			Probes: []configuration.ProbeConfig{},
		},
	}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	s := NewSensor(addDataPoint, mockProvider, baseLogger).(*sensor)
	if err := s.Start(make(chan struct{})); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mockProvider.config.Probes = []configuration.ProbeConfig{
		{
			Name:   "test-cpu",
			Type:   "cpu",
			Params: map[string]interface{}{"interval": 30},
		},
		{
			Name:   "test-memory",
			Type:   "memory",
			Params: map[string]interface{}{"interval": 30},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mockProvider.TriggerConfigChange("config changed")
		}()
	}
	wg.Wait()

	if len(s.startedProbes) != 2 {
		t.Errorf("Expected 2 probes after concurrent syncs, got %d", len(s.startedProbes))
	}
	seen := make(map[string]bool)
	for _, pp := range s.startedProbes {
		if seen[pp.ProbeId] {
			t.Errorf("Probe started twice: %s", pp.ProbeId)
		}
		seen[pp.ProbeId] = true
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestGetLoggerForProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)
	mockProvider := &MockConfigProvider{}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error {
		return nil
	}

	s := NewSensor(addDataPoint, mockProvider, baseLogger).(*sensor)

	probeConfig := configuration.ProbeConfig{
		Name: "test-probe",
		Type: "cpu",
	}

	probeLogger := s.getLoggerForProbe(probeConfig)
	if probeLogger == nil {
		t.Error("getLoggerForProbe returned nil")
	}
}

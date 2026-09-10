package data_store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// TestOnConfigRefreshed_ShutsDownRemovedStrategies pins the #260 leak
// fix: a strategy dropped by a config refresh must be Shutdown() —
// historically it was silently abandoned, leaking its listener port,
// connections and scheduler goroutines (and a recreated HTTP strategy
// then failed to bind).
func TestOnConfigRefreshed_ShutsDownRemovedStrategies(t *testing.T) {
	ds := newTestDataStoreWithEmptyConfig(t)

	removed := &MockStrategy{name: "doomed"}
	kept := &MockStrategy{name: "survivor"}
	initial := []SyncStrategy{removed, kept}
	ds.strategies.Store(&initial)

	// Empty storage config: the refresh drops every strategy.
	ds.OnConfigRefreshed("test-removal")

	if !removed.wasShutdown() {
		t.Error("strategy removed by config refresh was not Shutdown — port/goroutine leak (#260)")
	}
	if !kept.wasShutdown() {
		t.Error("second removed strategy was not Shutdown")
	}
	if got := len(ds.activeStrategies()); got != 0 {
		t.Errorf("expected empty strategy set after refresh, got %d", got)
	}
}

// TestStrategiesSnapshot_HotReloadUnderLoad reproduces the #260 race
// pattern under -race: probe goroutines hammer the datapoint callback
// while the config watcher rebuilds the strategy set. The historical
// plain slice was rebuilt in place mid-iteration.
func TestStrategiesSnapshot_HotReloadUnderLoad(t *testing.T) {
	ds := newTestDataStoreWithEmptyConfig(t)

	mock := &MockStrategy{name: "mock"}
	set := []SyncStrategy{mock}
	ds.strategies.Store(&set)

	callback := ds.GetCallback()
	router := &MockStrategyRouter{targets: []string{"mock"}}
	points := []datapoint.DataPoint{{Name: "m", Value: 1, Timestamp: time.Now()}}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers: continuous config refreshes (empty config → swaps to
	// empty set and back via direct Store, exercising both paths).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			ds.OnConfigRefreshed("hot-reload")
			fresh := []SyncStrategy{&MockStrategy{name: "mock"}}
			ds.strategies.Store(&fresh)
		}
		close(stop)
	}()

	// Readers: datapoint callbacks from several probe goroutines.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = callback(points, router)
				}
			}
		}()
	}
	wg.Wait()
}

// newTestDataStoreWithEmptyConfig builds a dataStore over the existing
// mock provider with no storage config.
func newTestDataStoreWithEmptyConfig(t *testing.T) *dataStore {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockConfig := &MockAgentConfig{authKey: "test-key", serverURL: "https://example.com"}
	provider := &MockConfigProvider{}
	ds, ok := NewDataStore(mockConfig, provider, baseLogger).(*dataStore)
	if !ok {
		t.Fatal("NewDataStore did not return *dataStore")
	}
	return ds
}

// TestOnConfigRefreshed_RecordsStrategyStartFailure pins #826: a
// configured strategy that refuses to start must become observable
// state, not just one ERR line at boot. The agent keeps running with
// its other outputs, which is exactly why the failure goes unnoticed.
func TestOnConfigRefreshed_RecordsStrategyStartFailure(t *testing.T) {
	agentstate.ResetStrategyFailuresForTest()
	t.Cleanup(agentstate.ResetStrategyFailuresForTest)

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockConfig := &MockAgentConfig{authKey: "k", serverURL: "https://example.com"}
	provider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{
				// No endpoint: the OTLP strategy refuses this configuration.
				{Name: "otlp", Params: configuration.StorageConfigParams{"compression": "gzip"}},
			},
		},
	}
	ds, _ := NewDataStore(mockConfig, provider, baseLogger).(*dataStore)

	ds.OnConfigRefreshed("broken-config")

	if got := len(ds.activeStrategies()); got != 0 {
		t.Fatalf("a strategy that refused its config is running: %d", got)
	}
	f, ok := agentstate.GetStrategyFailures()["otlp"]
	if !ok {
		t.Fatal("the rejected strategy left no failure state — invisible outage (#826)")
	}
	if f.Reason != agentstate.StrategyFailureInvalidConfig {
		t.Errorf("reason=%q, want %q", f.Reason, agentstate.StrategyFailureInvalidConfig)
	}
	if f.Detail == "" {
		t.Error("failure carries no detail; the operator cannot tell what to fix")
	}

	// Fixing the configuration clears the state without a restart.
	provider.config.StorageConfig = []configuration.StorageConfig{
		{Name: "otlp", Params: configuration.StorageConfigParams{"endpoint": "127.0.0.1:14998", "tls": map[string]interface{}{"enabled": false}}},
	}
	ds.OnConfigRefreshed("fixed-config")
	if _, still := agentstate.GetStrategyFailures()["otlp"]; still {
		t.Error("failure state survived a successful start")
	}
	_ = ds.Shutdown(context.Background())
}

// TestOnConfigRefreshed_InPlaceEditKeepsReplacementAlive pins the reload
// contract questioned by #827: editing one parameter of a strategy
// fragment in place must leave exactly ONE live instance, carrying the
// new parameters and still accepting data. The refresh legitimately
// logs a shutdown for the replaced instance and, in the cleanup pass,
// for that same instance again — both lines carry the same strategy
// name, which is what made the journal read as "the replacement was
// killed on arrival".
func TestOnConfigRefreshed_InPlaceEditKeepsReplacementAlive(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockConfig := &MockAgentConfig{authKey: "k", serverURL: "https://example.com"}
	provider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{
				{Name: "otlp", Params: configuration.StorageConfigParams{"endpoint": "127.0.0.1:14999", "compression": "gzip", "tls": map[string]interface{}{"enabled": false}}},
			},
		},
	}
	ds, _ := NewDataStore(mockConfig, provider, baseLogger).(*dataStore)

	ds.OnConfigRefreshed("initial")
	if got := len(ds.activeStrategies()); got != 1 {
		t.Fatalf("after initial refresh: %d strategies, want 1", got)
	}

	// Same fragment, one param changed (the compression:none case).
	provider.config.StorageConfig = []configuration.StorageConfig{
		{Name: "otlp", Params: configuration.StorageConfigParams{"endpoint": "127.0.0.1:14999", "compression": "none", "tls": map[string]interface{}{"enabled": false}}},
	}
	ds.OnConfigRefreshed("edited-in-place")

	act := ds.activeStrategies()
	t.Logf("after edit: %d strategies", len(act))
	for _, s := range act {
		t.Logf("  alive: %s", s.GetStrategyName())
	}
	if len(act) != 1 {
		t.Fatalf("after in-place edit: %d strategies, want 1", len(act))
	}
	if got := act[0].GetStrategyParams()["compression"]; got != "none" {
		t.Fatalf("alive strategy carries compression=%v, want none (the replacement did not take over)", got)
	}
	// The alive instance must still accept datapoints: a shut-down
	// strategy left in the router is the silent-death shape of #827.
	if err := act[0].AddDataPoints([]datapoint.DataPoint{{Name: "probe.metric", Value: 1, Timestamp: time.Now()}}); err != nil {
		t.Fatalf("alive strategy rejected data after reload: %v", err)
	}

	// A second refresh with the SAME config must be a no-op.
	ds.OnConfigRefreshed("no-change")
	if got := len(ds.activeStrategies()); got != 1 {
		t.Fatalf("after no-change refresh: %d strategies, want 1", got)
	}
	_ = ds.Shutdown(context.Background())
}

// An agent whose every output refused to start has nowhere to send what
// it collects, and when the refusal is the HTTP output there is not even
// a console to say so. It must refuse to run rather than look healthy.
func TestStartRefusesWhenNoOutputCouldStart(t *testing.T) {
	agentstate.ResetStrategyFailuresForTest()
	t.Cleanup(agentstate.ResetStrategyFailuresForTest)

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockConfig := &MockAgentConfig{authKey: "k", serverURL: "https://example.com"}
	provider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{
				// No endpoint: the OTLP strategy refuses this configuration.
				{Name: "otlp", Params: configuration.StorageConfigParams{"compression": "gzip"}},
			},
		},
	}
	ds, _ := NewDataStore(mockConfig, provider, baseLogger).(*dataStore)

	err := ds.Start(context.Background())
	if err == nil {
		t.Fatal("an agent with no working output must not start")
	}
	if !strings.Contains(err.Error(), "nothing would leave this host") {
		t.Errorf("the error must say what is wrong, got %v", err)
	}
}

// An output a poller reads cannot succeed later: the address it was
// refused will not free itself. Another output still working does not
// make up for it, because that is exactly the case where the console is
// the thing that went missing.
func TestStartRefusesWhenAPullOutputCannotTakeItsSocket(t *testing.T) {
	agentstate.ResetStrategyFailuresForTest()
	t.Cleanup(agentstate.ResetStrategyFailuresForTest)
	outputspec.Register(outputspec.Output{Type: "pulltest", DisplayName: "Pull test", Mode: outputspec.ModePull})
	agentstate.RecordStrategyFailure("pulltest", agentstate.StrategyFailureStart, "listen tcp 127.0.0.1:9094: bind: address already in use")

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	mockConfig := &MockAgentConfig{authKey: "k", serverURL: "https://example.com"}
	provider := &MockConfigProvider{
		config: configuration.ConfigurationData{
			StorageConfig: []configuration.StorageConfig{{Name: "pulltest"}},
		},
	}
	ds, _ := NewDataStore(mockConfig, provider, baseLogger).(*dataStore)

	err := ds.refuseToRunBlind()
	if err == nil {
		t.Fatal("a pull output that could not listen must stop the agent")
	}
	if !strings.Contains(err.Error(), "no retry will take the address") || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("the error must name the cause, got %v", err)
	}
}

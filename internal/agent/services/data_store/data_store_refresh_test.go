package data_store

import (
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
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

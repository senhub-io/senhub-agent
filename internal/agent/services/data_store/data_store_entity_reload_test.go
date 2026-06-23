package data_store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// entityReloadConfigProvider lets the test swap the storage config that
// OnConfigRefreshed reads, so we can drive a real OTLP-strategy recreate
// (endpoint change) the same way a config edit + hot-reload does.
type entityReloadConfigProvider struct {
	cfg atomic.Pointer[configuration.ConfigurationData]
}

func (p *entityReloadConfigProvider) set(c configuration.ConfigurationData) { p.cfg.Store(&c) }
func (p *entityReloadConfigProvider) GetConfiguration() configuration.ConfigurationData {
	return *p.cfg.Load()
}
func (p *entityReloadConfigProvider) OnConfigChanged(func(string)) {}
func (p *entityReloadConfigProvider) GetName() string              { return "entityReloadConfigProvider" }
func (p *entityReloadConfigProvider) Start(chan struct{}) error    { return nil }
func (p *entityReloadConfigProvider) Shutdown(context.Context) error {
	return nil
}

func otlpEntityStorageConfig(endpoint string) configuration.StorageConfig {
	return configuration.StorageConfig{
		Name: "otlp",
		Params: map[string]interface{}{
			"endpoint":    endpoint,
			"protocol":    "http",
			"compression": "none",
			"tls":         map[string]interface{}{"enabled": false},
			"retry":       map[string]interface{}{"enabled": false},
			"signals": map[string]interface{}{
				"metrics": map[string]interface{}{"enabled": false},
				"logs": map[string]interface{}{
					"enabled":       false,
					"batch_timeout": "20ms",
				},
				"entities": map[string]interface{}{
					"enabled":  true,
					"interval": "50ms",
				},
			},
		},
	}
}

// TestOnConfigRefreshed_OTLPEntityEmissionSurvivesReload reproduces #495: a
// config-reload that changes the OTLP endpoint recreates the strategy. The
// OTLP strategy owns process-global entity state (the source registry and the
// single entity detector). Before the fix, the replacement started while the
// old instance was still registered, so the global registry briefly held two
// instances' sources and two detectors ran. The fix tears the old instance
// down before the new one starts: after the reload there is exactly one
// strategy's worth of sources and entity events keep flowing on the new
// endpoint.
func TestOnConfigRefreshed_OTLPEntityEmissionSurvivesReload(t *testing.T) {
	entity.ResetForTest()
	t.Cleanup(entity.ResetForTest)

	var logHitsA, logHitsB atomic.Int64
	mkServer := func(counter *atomic.Int64) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/logs" {
				counter.Add(1)
			}
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	srvA := mkServer(&logHitsA)
	srvB := mkServer(&logHitsB)
	epA := strings.TrimPrefix(srvA.URL, "http://")
	epB := strings.TrimPrefix(srvB.URL, "http://")

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	provider := &entityReloadConfigProvider{}
	provider.set(configuration.ConfigurationData{
		StorageConfig: []configuration.StorageConfig{otlpEntityStorageConfig(epA)},
	})
	mockConfig := &MockAgentConfig{authKey: "test-key", serverURL: "https://example.com"}
	dsIface := NewDataStore(mockConfig, provider, baseLogger)
	ds := dsIface.(*dataStore)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ds.Shutdown(ctx)
	})

	// Initial bring-up: one OTLP strategy, entity emission on endpoint A.
	ds.OnConfigRefreshed("initial")

	if got := entity.RegisteredSourceCount(); got == 0 {
		t.Fatalf("initial start registered no entity sources")
	}
	initialSources := entity.RegisteredSourceCount()

	waitFor(t, &logHitsA, "endpoint A never received entity records")

	// Config reload: endpoint changes -> OTLP strategy recreated. Sample the
	// global source count at high frequency during the reload: the fix tears
	// the old instance down before the replacement registers, so the count
	// must never exceed one instance's worth. Without the fix the new
	// instance starts while the old is still registered, so the count
	// momentarily doubles and two detectors publish overlapping heartbeats.
	provider.set(configuration.ConfigurationData{
		StorageConfig: []configuration.StorageConfig{otlpEntityStorageConfig(epB)},
	})

	var maxDuringReload atomic.Int64
	stopSampler := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		for {
			select {
			case <-stopSampler:
				return
			default:
				if n := int64(entity.RegisteredSourceCount()); n > maxDuringReload.Load() {
					maxDuringReload.Store(n)
				}
			}
		}
	}()

	ds.OnConfigRefreshed("endpoint-change")
	close(stopSampler)
	<-samplerDone

	if got := maxDuringReload.Load(); got > int64(initialSources) {
		t.Fatalf("during reload the registry held %d entity sources, want <= %d — the old OTLP instance overlapped the new one (#495)", got, initialSources)
	}

	// Exactly one strategy's worth of sources after the reload: no overlap
	// (old left behind) and no leak.
	if got := entity.RegisteredSourceCount(); got != initialSources {
		t.Fatalf("after reload registered %d entity sources, want %d (one OTLP instance, no overlap/leak)", got, initialSources)
	}

	// Entity events must resume on the NEW endpoint without a manual restart.
	logHitsB.Store(0)
	waitFor(t, &logHitsB, "BUG #495: no entity records on the new endpoint after a config reload")
}

func waitFor(t *testing.T, counter *atomic.Int64, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

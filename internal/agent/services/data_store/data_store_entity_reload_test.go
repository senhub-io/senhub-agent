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
func (p *entityReloadConfigProvider) Start(context.Context) error  { return nil }
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

// TestOnConfigRefreshed_OTLPEntityPumpSurvivesReload is what remains of
// the #495 regression test, and it checks a different contract because
// the bug's cause no longer exists.
//
// #495 was: a reload that changes the OTLP endpoint recreates the
// strategy, and the strategy owned process-global entity state — the
// source registry and the single detector. The replacement started
// while the old one was still registered, so two detectors briefly ran.
//
// The producer now lives in the entitydetect service, at the agent's
// lifecycle rather than a strategy's, so a strategy recreate cannot
// duplicate it: the overlap this test used to sample for is structurally
// impossible (#932). What is left to verify is the CONSUMER half, which
// is still the strategy's: the pump subscribes to the neutral channel,
// and after a reload it must relay events to the new endpoint without a
// manual restart.
//
// Events are published straight onto the channel rather than produced by
// a detector: the pump's contract is "what arrives on the channel goes
// out", and testing it against a real producer would measure the
// producer too.
func TestOnConfigRefreshed_OTLPEntityPumpSurvivesReload(t *testing.T) {
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

	ds.OnConfigRefreshed("initial")

	// The strategy must no longer own any entity source: that ownership
	// is exactly what made a recreate dangerous.
	if got := entity.RegisteredSourceCount(); got != 0 {
		t.Fatalf("the OTLP strategy registered %d entity source(s); producing entities is the entitydetect service's job now (#932)", got)
	}

	publish := func() {
		entity.PublishEvent(entity.Event{
			Kind: entity.EntityState,
			Entity: &entity.Entity{
				Type:  "host",
				ID:    map[string]any{"host.id": "host-under-test"},
				Scope: entity.ScopeHostSvc,
			},
			Time:     time.Now(),
			Interval: time.Minute,
		})
	}

	publishUntil(t, publish, &logHitsA, "endpoint A never received the entity records the pump was given")

	provider.set(configuration.ConfigurationData{
		StorageConfig: []configuration.StorageConfig{otlpEntityStorageConfig(epB)},
	})
	ds.OnConfigRefreshed("endpoint-change")

	logHitsB.Store(0)
	publishUntil(t, publish, &logHitsB, "no entity records on the new endpoint after a config reload")
}

// publishUntil keeps feeding the channel until the counter moves: the
// pump subscribes as the strategy starts, and an event published before
// that subscription is simply not seen by anyone.
func publishUntil(t *testing.T, publish func(), counter *atomic.Int64, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		publish()
		if counter.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
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

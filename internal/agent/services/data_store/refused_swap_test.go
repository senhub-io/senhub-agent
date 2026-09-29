package data_store

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// A configuration the agent cannot use must not cost the output it
// replaces. It used to stop the running instance first and fail after:
// a PSK file the service could not read left the host with no Zabbix
// output until a restart, although the previous configuration worked.
func TestARefusedConfigurationKeepsThePreviousOutputRunning(t *testing.T) {
	built := []*MockStrategy{}
	RegisterStrategy("swaptest", func(params configuration.StorageConfigParams, _ StrategyDeps) (SyncStrategy, error) {
		if params["broken"] == true {
			return nil, errors.New("reading 'tls.psk_file': permission denied")
		}
		m := &MockStrategy{name: "swaptest", params: params}
		built = append(built, m)
		return m, nil
	})

	provider := &entityReloadConfigProvider{}
	set := func(p map[string]interface{}) {
		provider.set(configuration.ConfigurationData{StorageConfig: []configuration.StorageConfig{{Name: "swaptest", Params: p}}})
	}
	set(map[string]interface{}{"server": "zbx:10051"})
	ds := NewDataStore(&MockAgentConfig{authKey: "k"}, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*dataStore)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = ds.Shutdown(ctx)
	})
	ds.OnConfigRefreshed("initial")
	if len(built) != 1 || !built[0].started {
		t.Fatalf("the first configuration did not start an output")
	}
	running := built[0]

	set(map[string]interface{}{"server": "zbx:10051", "broken": true})
	ds.OnConfigRefreshed("refused")
	if running.shutdown {
		t.Fatal("a refused configuration stopped the output that was working")
	}
	if active := ds.activeStrategies(); len(active) != 1 || active[0] != SyncStrategy(running) {
		t.Fatalf("active outputs after the refusal = %v, want the previous instance", active)
	}

	// Once the cause is fixed, the next refresh applies the new configuration.
	set(map[string]interface{}{"server": "zbx:10051", "tls": "psk"})
	ds.OnConfigRefreshed("fixed")
	if !running.shutdown || len(built) != 2 || !built[1].started {
		t.Fatalf("the fixed configuration did not replace the previous one")
	}
}

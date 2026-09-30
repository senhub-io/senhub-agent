package data_store

import (
	"reflect"
	"sync"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

type retireSinkStrategy struct {
	MockStrategy
	retiredMu sync.Mutex
	retired   [][]string
}

func (r *retireSinkStrategy) ForgetProbes(names []string) {
	r.retiredMu.Lock()
	defer r.retiredMu.Unlock()
	r.retired = append(r.retired, names)
}

// A probe removed from probes.d, renamed or disabled must be retired from
// the outputs that keep its last values; a probe still configured, even
// with new parameters, must not (#997).
func TestOnConfigRefreshedRetiresProbesNoLongerRunning(t *testing.T) {
	ds := newTestDataStoreWithEmptyConfig(t)
	provider := ds.configProvider.(*MockConfigProvider)
	sink := &retireSinkStrategy{MockStrategy: MockStrategy{name: "sink"}}
	set := []SyncStrategy{sink}
	ds.strategies.Store(&set)

	disabled := false
	provider.config = configuration.ConfigurationData{
		StorageConfig: []configuration.StorageConfig{{Name: "sink"}},
		Probes: []configuration.ProbeConfig{
			{Name: "cpu", Type: "cpu"},
			{Name: "Old_Name", Type: "ping"},
			{Name: "disk", Type: "disk"},
			{Name: "gone", Type: "memory"},
		},
	}
	ds.OnConfigRefreshed("initial")
	if len(sink.retired) != 0 {
		t.Fatalf("the first refresh retired %v", sink.retired)
	}

	provider.config.Probes = []configuration.ProbeConfig{
		{Name: "cpu", Type: "cpu", Params: configuration.ProbeConfigParams{"interval": 30}},
		{Name: "new_name", Type: "ping"},
		{Name: "disk", Type: "disk", Enabled: &disabled},
	}
	ds.OnConfigRefreshed("probes.d changed")

	want := [][]string{{"disk", "gone", "old_name"}}
	if !reflect.DeepEqual(sink.retired, want) {
		t.Fatalf("retired %v, want %v", sink.retired, want)
	}
	if sink.wasShutdown() {
		t.Fatal("the output itself was shut down")
	}
}

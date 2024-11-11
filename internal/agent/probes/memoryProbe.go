package probes

import (
	"context"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
)

type memoryProbe struct {
	config *configuration.RemoteConfiguration
}

func NewMemoryProbe(config *configuration.RemoteConfiguration) Probe {
	return &memoryProbe{
		config: config,
	}
}

func (m *memoryProbe) GetName() string {
	return "host_memory"
}
func (m *memoryProbe) ShouldStart() bool {
	return true
}
func (m *memoryProbe) GetInterval() time.Duration {
	return 2 * time.Second
}
func (m *memoryProbe) Collect() ([]data_store.DataPoint, error) {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	var timestamp = time.Now()

	return []data_store.DataPoint{
		{Name: "mem_alloc", Timestamp: timestamp, Value: float32(s.Alloc)},
		{Name: "mem_total_alloc", Timestamp: timestamp, Value: float32(s.TotalAlloc)},
		{Name: "mem_sys", Timestamp: timestamp, Value: float32(s.Sys)},
		{Name: "mem_num_gc", Timestamp: timestamp, Value: float32(s.NumGC)},
	}, nil
}
func (m *memoryProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *memoryProbe) OnShutdown(ctx context.Context) error {
	return nil
}

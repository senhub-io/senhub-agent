package probes

import (
	"context"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type memoryProbe struct {
	config map[string]interface{}
	logger *logger.Logger
}

func NewMemoryProbe(config map[string]interface{}, logger *logger.Logger) Probe {
    return &memoryProbe{
        config: config,
        logger: logger,
    }
}

func (m *memoryProbe) GetName() string {
	return "host_memory"
}
func (m *memoryProbe) ShouldStart() bool {
	return true
}
func (m *memoryProbe) ValidateConfig(config map[string]interface{}) bool {
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

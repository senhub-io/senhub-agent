package probes

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

type memoryProbe struct {
}

func NewMemoryProbe() Probe {
	return &memoryProbe{}
}

func (m *memoryProbe) GetName() string {
	return "MemoryProbe"
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

	return []data_store.DataPoint{
		{Name: "mem_alloc", Value: fmt.Sprintf("%d", s.Alloc)},
		{Name: "mem_total_alloc", Value: fmt.Sprintf("%d", s.TotalAlloc)},
		{Name: "mem_sys", Value: fmt.Sprintf("%d", s.Sys)},
		{Name: "mem_num_gc", Value: fmt.Sprintf("%d", s.NumGC)},
	}, nil
}
func (m *memoryProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *memoryProbe) OnShutdown(ctx context.Context) error {
	return nil
}

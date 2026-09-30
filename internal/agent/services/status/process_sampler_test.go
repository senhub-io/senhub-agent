package status

import (
	"runtime"
	"testing"
	"time"
)

func TestProcessSampler_MeasuresTheProcess(t *testing.T) {
	s := newProcessSampler()
	s.minInterval = 0

	deadline := time.Now().Add(300 * time.Millisecond)
	x := 0
	for time.Now().Before(deadline) {
		x++
	}
	_ = x
	got := s.sample()
	if !got.OK {
		t.Skip("the operating system gave no process information here")
	}
	if got.RSSMB <= 0 {
		t.Errorf("resident size = %v MB, want > 0", got.RSSMB)
	}
	if got.CPUPercent <= 0 {
		t.Errorf("CPU = %v %% after a busy loop, want > 0", got.CPUPercent)
	}
	if got.CPUPercent > 100 {
		t.Errorf("CPU = %v %%, a share of the machine cannot exceed 100", got.CPUPercent)
	}
	if got.Started.IsZero() || got.Started.After(time.Now()) {
		t.Errorf("process start = %v", got.Started)
	}
}

func TestPerformanceInfo_ResidentNotHeap(t *testing.T) {
	svc := &StatusService{startTime: time.Now(), process: newProcessSampler()}
	perf := svc.calculatePerformanceInfo()
	if !perf.Measured {
		t.Skip("no process information here")
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if perf.MemoryUsageMB <= perf.HeapMB {
		t.Errorf("resident %.1f MB not above Go heap %.1f MB", perf.MemoryUsageMB, perf.HeapMB)
	}
}

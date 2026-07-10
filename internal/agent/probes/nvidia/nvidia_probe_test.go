package nvidia

import (
	"errors"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg := parseConfig(map[string]interface{}{})
	if cfg.NvidiaSmiPath != defaultNvidiaSmiPath {
		t.Errorf("expected default smi path %q, got %q", defaultNvidiaSmiPath, cfg.NvidiaSmiPath)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("expected default interval %v, got %v", defaultInterval, cfg.Interval)
	}
	if len(cfg.GPUs) != 0 {
		t.Errorf("expected empty GPUs, got %v", cfg.GPUs)
	}
}

func TestParseConfig_Overrides(t *testing.T) {
	cfg := parseConfig(map[string]interface{}{
		"nvidia_smi_path": "/usr/local/bin/nvidia-smi",
		"interval":        60,
		"gpus":            []interface{}{"0", "1"},
	})
	if cfg.NvidiaSmiPath != "/usr/local/bin/nvidia-smi" {
		t.Errorf("nvidia_smi_path not set: %q", cfg.NvidiaSmiPath)
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("interval not set: %v", cfg.Interval)
	}
	if len(cfg.GPUs) != 2 || cfg.GPUs[0] != "0" || cfg.GPUs[1] != "1" {
		t.Errorf("gpus not set: %v", cfg.GPUs)
	}
}

const smiSample = `0, GeForce RTX 4090, GPU-abc123, 45, 60, 8192, 24576, 72, 350.5, 450, 35, 1, 10, 5
1, Tesla T4, GPU-def456, 0, 0, 512, 16384, 35, [N/A], [N/A], [N/A], 0, 0, 0`

func TestParseSmiOutput_TwoGPUs(t *testing.T) {
	gpus, err := parseSmiOutput([]byte(smiSample))
	if err != nil {
		t.Fatalf("parseSmiOutput: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}

	g0 := gpus[0]
	if g0.index != "0" {
		t.Errorf("index: got %q, want %q", g0.index, "0")
	}
	if g0.name != "GeForce RTX 4090" {
		t.Errorf("name: got %q", g0.name)
	}
	if g0.uuid != "GPU-abc123" {
		t.Errorf("uuid: got %q", g0.uuid)
	}
	if g0.utilizationGPU != 45 {
		t.Errorf("utilizationGPU: got %v, want 45", g0.utilizationGPU)
	}
	if g0.memoryUsedMiB != 8192 {
		t.Errorf("memoryUsedMiB: got %v, want 8192", g0.memoryUsedMiB)
	}
	if g0.memoryTotalMiB != 24576 {
		t.Errorf("memoryTotalMiB: got %v, want 24576", g0.memoryTotalMiB)
	}
	if g0.temperatureGPU != 72 {
		t.Errorf("temperature: got %v, want 72", g0.temperatureGPU)
	}
	if g0.powerDraw != 350.5 {
		t.Errorf("powerDraw: got %v, want 350.5", g0.powerDraw)
	}
	if g0.powerLimit != 450 {
		t.Errorf("powerLimit: got %v, want 450", g0.powerLimit)
	}
	if g0.fanSpeed != 35 {
		t.Errorf("fanSpeed: got %v, want 35", g0.fanSpeed)
	}
	if g0.utilizationEncoder != 10 {
		t.Errorf("utilizationEncoder: got %v, want 10", g0.utilizationEncoder)
	}
	if g0.utilizationDecoder != 5 {
		t.Errorf("utilizationDecoder: got %v, want 5", g0.utilizationDecoder)
	}

	// Tesla T4: power and fan are N/A
	g1 := gpus[1]
	if g1.powerDraw != -1 {
		t.Errorf("T4 powerDraw: got %v, want -1 (N/A)", g1.powerDraw)
	}
	if g1.powerLimit != -1 {
		t.Errorf("T4 powerLimit: got %v, want -1 (N/A)", g1.powerLimit)
	}
	if g1.fanSpeed != 0 {
		t.Errorf("T4 fanSpeed: got %v, want 0", g1.fanSpeed)
	}
}

func TestParseSmiOutput_Empty(t *testing.T) {
	gpus, err := parseSmiOutput([]byte{})
	if err != nil {
		t.Errorf("unexpected error on empty input: %v", err)
	}
	if len(gpus) != 0 {
		t.Errorf("expected empty slice, got %v", gpus)
	}
}

func TestParseSmiOutput_TooFewColumns(t *testing.T) {
	_, err := parseSmiOutput([]byte("0, RTX 4090, GPU-abc, 45"))
	if err == nil {
		t.Error("expected error on too few columns")
	}
	if !strings.Contains(err.Error(), "columns") {
		t.Errorf("error does not mention columns: %v", err)
	}
}

func TestFilterGPUs_AllowAll(t *testing.T) {
	gpus := []nvidiaGPU{{index: "0"}, {index: "1"}}
	out := filterGPUs(gpus, nil)
	if len(out) != 2 {
		t.Errorf("expected 2, got %d", len(out))
	}
}

func TestFilterGPUs_Subset(t *testing.T) {
	gpus := []nvidiaGPU{{index: "0"}, {index: "1"}, {index: "2"}}
	out := filterGPUs(gpus, []string{"0", "2"})
	if len(out) != 2 {
		t.Errorf("expected 2, got %d", len(out))
	}
	if out[0].index != "0" || out[1].index != "2" {
		t.Errorf("unexpected indices: %v", out)
	}
}

func TestBuildDatapoints_RTX4090(t *testing.T) {
	gpu := nvidiaGPU{
		index: "0", name: "GeForce RTX 4090", uuid: "GPU-abc123",
		utilizationGPU: 45, utilizationMemory: 60,
		memoryUsedMiB: 8192, memoryTotalMiB: 24576,
		temperatureGPU: 72, powerDraw: 350.5, powerLimit: 450,
		fanSpeed: 35, utilizationEncoder: 10, utilizationDecoder: 5,
	}
	now := time.Now()
	pts := buildDatapoints(gpu, nil, now)

	find := func(name string) (float64, bool) {
		for _, p := range pts {
			if p.Name == name {
				return p.Value, true
			}
		}
		return 0, false
	}

	if v, ok := find("senhub.nvidia.up"); !ok || v != 1 {
		t.Errorf("senhub.nvidia.up: got %v ok=%v", v, ok)
	}
	if v, ok := find("gpu.utilization"); !ok || v != float64(45) {
		t.Errorf("gpu.utilization: got %v ok=%v, want 45", v, ok)
	}
	// memory: 8192 MiB → 8192 * 1024 * 1024 bytes
	wantMemUsed := float64(8192 * 1024 * 1024)
	if v, ok := find("gpu.memory.used"); !ok || v != wantMemUsed {
		t.Errorf("gpu.memory.used: got %v ok=%v, want %v", v, ok, wantMemUsed)
	}
	if v, ok := find("gpu.memory.utilization"); !ok || v != float64(60) {
		t.Errorf("gpu.memory.utilization: got %v ok=%v, want 60", v, ok)
	}
	if v, ok := find("gpu.temperature"); !ok || v != 72 {
		t.Errorf("gpu.temperature: got %v ok=%v", v, ok)
	}
	if v, ok := find("gpu.power.usage"); !ok || v != float64(350.5) {
		t.Errorf("gpu.power.usage: got %v ok=%v", v, ok)
	}
	if v, ok := find("gpu.power.limit"); !ok || v != float64(450) {
		t.Errorf("gpu.power.limit: got %v ok=%v", v, ok)
	}
	if v, ok := find("gpu.fan.speed"); !ok || v != float64(35) {
		t.Errorf("gpu.fan.speed: got %v ok=%v, want 35", v, ok)
	}
	if v, ok := find("gpu.encoder.utilization"); !ok || v != float64(10) {
		t.Errorf("gpu.encoder.utilization: got %v ok=%v, want 10", v, ok)
	}
	if v, ok := find("gpu.decoder.utilization"); !ok || v != float64(5) {
		t.Errorf("gpu.decoder.utilization: got %v ok=%v, want 5", v, ok)
	}
}

func TestBuildDatapoints_PowerNA(t *testing.T) {
	gpu := nvidiaGPU{
		index: "0", name: "Tesla T4", uuid: "GPU-xyz",
		powerDraw: -1, powerLimit: -1,
	}
	pts := buildDatapoints(gpu, nil, time.Now())
	for _, p := range pts {
		if p.Name == "gpu.power.usage" || p.Name == "gpu.power.limit" {
			t.Errorf("power metric %q should not be emitted when N/A", p.Name)
		}
	}
}

func TestCollect_SmiFailure(t *testing.T) {
	probe, err := NewNvidiaProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}
	p := probe.(*NvidiaProbe)
	p.SetName("nvidia-test")
	p.runSmi = func(path string) ([]byte, error) {
		return nil, errors.New("nvidia-smi not found")
	}

	pts, err := probe.Collect()
	if err != nil {
		t.Errorf("expected nil error on smi failure, got: %v", err)
	}
	found := false
	for _, dp := range pts {
		if dp.Name == "senhub.nvidia.up" && dp.Value == 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected senhub.nvidia.up=0 on smi failure")
	}
}

func TestCollect_TwoGPUs(t *testing.T) {
	probe, err := NewNvidiaProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}
	p := probe.(*NvidiaProbe)
	p.SetName("nvidia-test")
	p.runSmi = func(path string) ([]byte, error) {
		return []byte(smiSample), nil
	}

	pts, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// Count senhub.nvidia.up datapoints — one per GPU
	upCount := 0
	for _, dp := range pts {
		if dp.Name == "senhub.nvidia.up" && dp.Value == 1 {
			upCount++
		}
	}
	if upCount != 2 {
		t.Errorf("expected 2 senhub.nvidia.up=1 points (one per GPU), got %d", upCount)
	}
}

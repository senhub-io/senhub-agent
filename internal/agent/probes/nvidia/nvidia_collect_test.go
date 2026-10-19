package nvidia

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

func newNvidiaCollectProbe(t *testing.T, raw map[string]interface{}, run runSmiFunc) *NvidiaProbe {
	t.Helper()

	probe, err := NewNvidiaProbe(raw, testLogger())
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}
	p, ok := probe.(*NvidiaProbe)
	if !ok {
		t.Fatal("returned probe is not *NvidiaProbe")
	}
	p.SetName("nvidia-collect-test")
	p.runSmi = run
	return p
}

func collectNvidiaPoints(t *testing.T, p *NvidiaProbe) []data_store.DataPoint {
	t.Helper()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return points
}

func nvidiaPointValuesByName(points []data_store.DataPoint) map[string][]float64 {
	got := make(map[string][]float64)
	for _, point := range points {
		got[point.Name] = append(got[point.Name], point.Value)
	}
	return got
}

func writeNvidiaSmiStub(t *testing.T, dir, script string) {
	t.Helper()

	path := filepath.Join(dir, "nvidia-smi")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing nvidia-smi stub: %v", err)
	}
}

func TestNewNvidiaProbe_ConfigAndIdentity(t *testing.T) {
	probe, err := NewNvidiaProbe(map[string]interface{}{
		"nvidia_smi_path": "/opt/bin/nvidia-smi",
		"interval":        5,
		"gpus":            []string{"0", "3"},
	}, testLogger())
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}

	p, ok := probe.(*NvidiaProbe)
	if !ok {
		t.Fatal("returned probe is not *NvidiaProbe")
	}
	if p.config.NvidiaSmiPath != "/opt/bin/nvidia-smi" {
		t.Errorf("NvidiaSmiPath = %q, want /opt/bin/nvidia-smi", p.config.NvidiaSmiPath)
	}
	if p.config.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", p.config.Interval)
	}
	if len(p.config.GPUs) != 2 || p.config.GPUs[0] != "0" || p.config.GPUs[1] != "3" {
		t.Errorf("GPUs = %v, want [0 3]", p.config.GPUs)
	}
	if p.runSmi == nil {
		t.Error("runSmi should default to the production runner")
	}
}

func TestNvidiaProbe_TargetStrategies_ReturnsSupportedSinks(t *testing.T) {
	p := &NvidiaProbe{}
	got := p.GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}

	if len(got) != len(want) {
		t.Fatalf("GetTargetStrategies returned %d sinks, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sink[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNvidiaProbe_ShouldStart_AlwaysTrue(t *testing.T) {
	p := &NvidiaProbe{}
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
}

func TestNvidiaProbe_GetInterval_UsesConfiguredInterval(t *testing.T) {
	p := &NvidiaProbe{config: probeConfig{Interval: 9 * time.Second}}
	if got := p.GetInterval(); got != 9*time.Second {
		t.Errorf("GetInterval() = %v, want 9s", got)
	}
}

func TestNvidiaProbe_OnStart_ReturnsNil(t *testing.T) {
	probe, err := NewNvidiaProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}
	p, ok := probe.(*NvidiaProbe)
	if !ok {
		t.Fatal("returned probe is not *NvidiaProbe")
	}

	if err := p.OnStart(make(chan struct{})); err != nil {
		t.Errorf("OnStart returned error: %v", err)
	}
}

func TestNvidiaProbe_OnShutdown_ReturnsNil(t *testing.T) {
	p := &NvidiaProbe{}
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown returned error: %v", err)
	}
}

func TestCollect_EmptyOutputReturnsNoPoints(t *testing.T) {
	p := newNvidiaCollectProbe(t, map[string]interface{}{}, func(path string) ([]byte, error) {
		return []byte{}, nil
	})

	points := collectNvidiaPoints(t, p)
	if len(points) != 0 {
		t.Errorf("Collect returned %d datapoints for empty nvidia-smi output, want 0", len(points))
	}
}

func TestCollect_GPUsFilterSelectsNothingReturnsNoPoints(t *testing.T) {
	p := newNvidiaCollectProbe(t, map[string]interface{}{"gpus": []interface{}{"5"}}, func(path string) ([]byte, error) {
		return []byte(smiSample), nil
	})

	points := collectNvidiaPoints(t, p)
	if len(points) != 0 {
		t.Errorf("Collect returned %d datapoints when the GPU allow-list selects nothing, want 0", len(points))
	}
}

func TestCollect_ParseErrorMidOutputEmitsUpZero(t *testing.T) {
	badOutput := "0, GeForce RTX 4090, GPU-abc123, 45, 60, 8192, 24576, 72, 350.5, 450, 35, 1, 10, 5\n1, Tesla T4, GPU-def"
	p := newNvidiaCollectProbe(t, map[string]interface{}{}, func(path string) ([]byte, error) {
		return []byte(badOutput), nil
	})

	points := collectNvidiaPoints(t, p)
	if len(points) != 1 {
		t.Fatalf("Collect returned %d datapoints, want 1", len(points))
	}

	dp := points[0]
	if dp.Name != "senhub.nvidia.up" || dp.Value != 0 {
		t.Errorf("datapoint = %s=%v, want senhub.nvidia.up=0", dp.Name, dp.Value)
	}

	hasAvailabilityTag := false
	for _, tag := range dp.Tags {
		if tag.Key == "metric_type" && tag.Value == "availability" {
			hasAvailabilityTag = true
		}
	}
	if !hasAvailabilityTag {
		t.Error("senhub.nvidia.up=0 missing metric_type=availability tag")
	}
}

func TestCollect_NAFieldsAreParsedAndReported(t *testing.T) {
	output := "0, Tesla T4, GPU-t4, 0, 0, 512, 16384, 35, [Not Supported], [N/A], [N/A], 0, 0, 0\n"
	p := newNvidiaCollectProbe(t, map[string]interface{}{}, func(path string) ([]byte, error) {
		return []byte(output), nil
	})

	points := collectNvidiaPoints(t, p)
	got := nvidiaPointValuesByName(points)

	want := map[string]float64{
		"senhub.nvidia.up":        1,
		"gpu.utilization":         0,
		"gpu.memory.used":         float64(512 * 1024 * 1024),
		"gpu.memory.total":        float64(16384 * 1024 * 1024),
		"gpu.memory.utilization":  0,
		"gpu.temperature":         35,
		"gpu.encoder.utilization": 0,
		"gpu.decoder.utilization": 0,
	}
	if values := got["gpu.fan.speed"]; len(values) != 0 {
		t.Errorf("gpu.fan.speed reported as N/A must be left out, got %v", values)
	}
	for metric, wantValue := range want {
		values := got[metric]
		if len(values) != 1 {
			t.Errorf("%s: got %v values, want 1", metric, values)
			continue
		}
		if values[0] != wantValue {
			t.Errorf("%s = %v, want %v", metric, values[0], wantValue)
		}
	}

	if values := got["gpu.power.usage"]; len(values) != 0 {
		t.Errorf("gpu.power.usage should not be emitted for [Not Supported], got %v", values)
	}
	if values := got["gpu.power.limit"]; len(values) != 0 {
		t.Errorf("gpu.power.limit should not be emitted for [N/A], got %v", values)
	}
}

func TestDefaultRunSmi_CommandRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script nvidia-smi stub requires unix")
	}

	successScript := "#!/bin/sh\nprintf '0, GeForce RTX 4090, GPU-abc123, 45, 60, 8192, 24576, 72, 350.5, 450, 35, 1, 10, 5\\n1, Tesla T4, GPU-def456, 0, 0, 512, 16384, 35, [N/A], [N/A], [N/A], 0, 0, 0'\n"
	failScript := "#!/bin/sh\nexit 2\n"

	tests := []struct {
		name       string
		script     string
		wantErr    string
		wantOutput string
	}{
		{name: "success", script: successScript, wantOutput: smiSample},
		{name: "non-zero exit", script: failScript, wantErr: "exit status 2"},
		{name: "missing binary", script: "", wantErr: "exec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.script != "" {
				writeNvidiaSmiStub(t, dir, tt.script)
			}
			t.Setenv("PATH", dir)

			out, err := defaultRunSmi(defaultNvidiaSmiPath)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("defaultRunSmi returned error: %v", err)
				}
				if string(out) != tt.wantOutput {
					t.Errorf("output = %q, want %q", string(out), tt.wantOutput)
				}
			} else {
				if err == nil {
					t.Fatalf("defaultRunSmi succeeded, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

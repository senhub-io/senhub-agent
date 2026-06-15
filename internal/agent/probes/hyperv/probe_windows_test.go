//go:build windows

package hyperv

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// makeWMIFake returns a wmiQueryFn that serves synthetic data for tests.
// It matches on whether the query targets Msvm_ComputerSystem or not.
func makeWMIFake(vms []msvmComputerSystem, sums []msvmSummaryInformation, failVMs, failSums bool) wmiQueryFn {
	return func(query string, dst interface{}, _ string) error {
		if strings.Contains(query, "Msvm_ComputerSystem") {
			if failVMs {
				return fmt.Errorf("fake WMI: Msvm_ComputerSystem unavailable")
			}
			if p, ok := dst.(*[]msvmComputerSystem); ok {
				*p = vms
			}
			return nil
		}
		// Msvm_SummaryInformation
		if failSums {
			return fmt.Errorf("fake WMI: Msvm_SummaryInformation unavailable")
		}
		if p, ok := dst.(*[]msvmSummaryInformation); ok {
			*p = sums
		}
		return nil
	}
}

func newTestProbe(t *testing.T, fn wmiQueryFn) *HypervProbe {
	t.Helper()
	baseLogger := logger.NewLogger(nil)
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       probeConfig{Interval: defaultInterval},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
		queryFn:      fn,
		entitySrc:    newHypervEntitySource(),
	}
	p.SetProbeType(ProbeType)
	p.SetName("hyperv-test")
	return p
}

func TestCollect_NoVMs(t *testing.T) {
	fn := makeWMIFake(nil, nil, false, false)
	p := newTestProbe(t, fn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	// senhub.hyperv.up + 3 vm.count buckets = 4 minimum
	if len(points) < 4 {
		t.Errorf("expected at least 4 datapoints, got %d", len(points))
	}
	for _, pt := range points {
		if pt.Name == "senhub.hyperv.up" && pt.Value != 1 {
			t.Errorf("expected up=1 when WMI succeeds, got %v", pt.Value)
		}
	}
}

func TestCollect_WMIFailure(t *testing.T) {
	fn := makeWMIFake(nil, nil, true, false)
	p := newTestProbe(t, fn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not propagate WMI errors (got: %v)", err)
	}
	for _, pt := range points {
		if pt.Name == "senhub.hyperv.up" && pt.Value != 0 {
			t.Errorf("expected up=0 when WMI fails, got %v", pt.Value)
		}
	}
}

func TestBuildVMPoints_CPUNormalisation(t *testing.T) {
	baseLogger := logger.NewLogger(nil)
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "guid-1", EnabledState: enabledStateRunning}}
	sums := map[string]msvmSummaryInformation{
		"guid-1": {Name: "guid-1", ElementName: "TestVM", CPUUsage: 100, MemoryUsage: 1024},
	}
	points := p.buildVMPoints(vms, sums, time.Now())

	found := false
	for _, pt := range points {
		if pt.Name == "hyperv.vm.cpu.usage" {
			found = true
			if pt.Value != 1.0 {
				t.Errorf("100%% CPU should normalise to 1.0, got %v", pt.Value)
			}
		}
	}
	if !found {
		t.Error("no hyperv.vm.cpu.usage datapoint emitted")
	}
}

func TestBuildVMPoints_MemoryBytes(t *testing.T) {
	baseLogger := logger.NewLogger(nil)
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "g1", EnabledState: enabledStateRunning}}
	sums := map[string]msvmSummaryInformation{
		"g1": {Name: "g1", CPUUsage: 0, MemoryUsage: 2048},
	}
	points := p.buildVMPoints(vms, sums, time.Now())

	want := float32(2048 * 1024 * 1024)
	for _, pt := range points {
		if pt.Name == "hyperv.vm.memory.usage" && pt.Value != want {
			t.Errorf("expected memory %v bytes, got %v", want, pt.Value)
		}
	}
}

func TestBuildVMPoints_StateRunning(t *testing.T) {
	baseLogger := logger.NewLogger(nil)
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "vm1", EnabledState: enabledStateRunning}}
	points := p.buildVMPoints(vms, nil, time.Now())

	for _, pt := range points {
		if pt.Name == "hyperv.vm.state" && pt.Value != 1 {
			t.Errorf("running VM should have state=1, got %v", pt.Value)
		}
	}
}

func TestBuildVMPoints_CountBuckets(t *testing.T) {
	baseLogger := logger.NewLogger(nil)
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{
		{Name: "v1", EnabledState: enabledStateRunning},
		{Name: "v2", EnabledState: enabledStateStopped},
		{Name: "v3", EnabledState: enabledStatePaused},
	}
	points := p.buildVMPoints(vms, nil, time.Now())

	counts := map[string]float32{}
	for _, pt := range points {
		if pt.Name == "hyperv.vm.count" {
			for _, tg := range pt.Tags {
				if tg.Key == "state" {
					counts[tg.Value] = pt.Value
				}
			}
		}
	}
	cases := map[string]float32{"running": 1, "stopped": 1, "paused": 1}
	for state, want := range cases {
		if got := counts[state]; got != want {
			t.Errorf("count[%s]: expected %v, got %v", state, want, got)
		}
	}
}

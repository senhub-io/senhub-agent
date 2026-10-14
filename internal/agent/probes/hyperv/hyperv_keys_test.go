//go:build windows

package hyperv

import (
	"fmt"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func newTestHypervProbe(queryFn wmiQueryFn) *HypervProbe {
	probe, err := NewHypervProbe(map[string]interface{}{}, nil)
	if err != nil {
		panic(err)
	}
	p := probe.(*HypervProbe)
	p.queryFn = queryFn
	p.entitySource = newHypervEntitySource("hyperv-host-1", p.moduleLogger)
	p.entitySource.resolveGuestMachineID = func(string) string { return "" }
	p.SetEntitySource(p.entitySource)
	return p
}

func successfulQueryFn() wmiQueryFn {
	return func(_ string, dst interface{}, _ string) error {
		switch d := dst.(type) {
		case *[]msvmComputerSystem:
			*d = []msvmComputerSystem{
				{Name: "vm-guid-1", EnabledState: enabledStateRunning},
				{Name: "vm-guid-2", EnabledState: enabledStateStopped},
			}
		case *[]msvmSummaryInformation:
			*d = []msvmSummaryInformation{
				{Name: "vm-guid-1", ElementName: "web-1", NumberOfProcessors: 2, ProcessorLoad: 10, MemoryUsage: 1024, UpTime: 100},
				{Name: "vm-guid-2", ElementName: "db-1", NumberOfProcessors: 4, ProcessorLoad: 20, MemoryUsage: 2048, UpTime: 200},
			}
		}
		return nil
	}
}

func failingQueryFn() wmiQueryFn {
	return func(_ string, _ interface{}, _ string) error {
		return fmt.Errorf("connection refused")
	}
}

func tagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func findPoint(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	for _, p := range points {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("datapoint %q not found", name)
	return data_store.DataPoint{}
}

func TestCollect_KeysAndCheckResult_Success(t *testing.T) {
	p := newTestHypervProbe(successfulQueryFn())
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	for _, pt := range points {
		hostID, ok := tagValue(pt.Tags, "host.id")
		if !ok {
			t.Errorf("datapoint %q missing host.id: %+v", pt.Name, pt.Tags)
		} else if hostID != "hyperv-host-1" {
			t.Errorf("datapoint %q host.id = %q, want %q", pt.Name, hostID, "hyperv-host-1")
		}
	}

	vmIDs := map[string]bool{}
	for _, pt := range points {
		if pt.Name != "hyperv.vm.state" {
			continue
		}
		vmid, ok := tagValue(pt.Tags, "vmid")
		if !ok {
			t.Errorf("hyperv.vm.state missing vmid: %+v", pt.Tags)
			continue
		}
		vmIDs[vmid] = true
	}
	if len(vmIDs) != 2 {
		t.Fatalf("expected vmid keys for 2 VMs, got %v", vmIDs)
	}
	if !vmIDs["vm-guid-1"] || !vmIDs["vm-guid-2"] {
		t.Fatalf("expected vm-guid-1 and vm-guid-2 in per-VM points, got %v", vmIDs)
	}

	check := findPoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != 0 {
		t.Errorf("check result = %v, want 0", check.Value)
	}
	if mt, ok := tagValue(check.Tags, "metric_type"); !ok || mt != "status" {
		t.Errorf("check result metric_type = %q, ok=%v, want status", mt, ok)
	}
}

func TestCollect_CheckResult_TargetDown(t *testing.T) {
	p := newTestHypervProbe(failingQueryFn())
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	up := findPoint(t, points, "senhub.hyperv.up")
	if up.Value != 0 {
		t.Errorf("up value = %v, want 0", up.Value)
	}
	if hostID, ok := tagValue(up.Tags, "host.id"); !ok || hostID != "hyperv-host-1" {
		t.Errorf("up host.id = %q, ok=%v, want hyperv-host-1", hostID, ok)
	}

	check := findPoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != 1 {
		t.Errorf("check result = %v, want 1", check.Value)
	}
	if hostID, ok := tagValue(check.Tags, "host.id"); !ok || hostID != "hyperv-host-1" {
		t.Errorf("check result host.id = %q, ok=%v, want hyperv-host-1", hostID, ok)
	}
	if mt, ok := tagValue(check.Tags, "metric_type"); !ok || mt != "status" {
		t.Errorf("check result metric_type = %q, ok=%v, want status", mt, ok)
	}
}

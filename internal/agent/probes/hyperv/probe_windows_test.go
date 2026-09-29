//go:build windows

package hyperv

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
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
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.hyperv")
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       probeConfig{Interval: defaultInterval},
		moduleLogger: moduleLogger,
		queryFn:      fn,
		entitySource: newHypervEntitySource("test-host-id", moduleLogger),
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
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "guid-1", EnabledState: enabledStateRunning}}
	sums := map[string]msvmSummaryInformation{
		"guid-1": {Name: "guid-1", ElementName: "TestVM", ProcessorLoad: 100, MemoryUsage: 1024},
	}
	points := p.buildVMPoints(vms, sums, time.Now(), nil)

	found := false
	for _, pt := range points {
		if pt.Name == "hyperv.vm.cpu.usage" {
			found = true
			if pt.Value != 100.0 {
				t.Errorf("100%% CPU should be emitted as 100, got %v", pt.Value)
			}
		}
	}
	if !found {
		t.Error("no hyperv.vm.cpu.usage datapoint emitted")
	}
}

// TestBuildVMPoints_CarriesHostTags pins that host.id flows onto every VM
// datapoint so the telemetry joins the hypervisor host entity (Hyper-V is a
// host facet, #456). Without it the VM metrics would float uncorrelated.
func TestBuildVMPoints_CarriesHostTags(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	hostTags := []tags.Tag{{Key: "host.id", Value: "host-uuid-9"}}
	vms := []msvmComputerSystem{{Name: "g1", EnabledState: enabledStateRunning}}
	points := p.buildVMPoints(vms, nil, time.Now(), hostTags)

	if len(points) == 0 {
		t.Fatal("no datapoints emitted")
	}
	for _, pt := range points {
		var hasHost bool
		for _, tg := range pt.Tags {
			if tg.Key == "host.id" && tg.Value == "host-uuid-9" {
				hasHost = true
			}
		}
		if !hasHost {
			t.Errorf("datapoint %q missing host.id tag — VM telemetry will not join the host", pt.Name)
		}
	}
}

func TestBuildVMPoints_MemoryBytes(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "g1", EnabledState: enabledStateRunning}}
	sums := map[string]msvmSummaryInformation{
		"g1": {Name: "g1", ProcessorLoad: 0, MemoryUsage: 2048},
	}
	points := p.buildVMPoints(vms, sums, time.Now(), nil)

	want := float64(2048 * 1024 * 1024)
	for _, pt := range points {
		if pt.Name == "hyperv.vm.memory.usage" && pt.Value != want {
			t.Errorf("expected memory %v bytes, got %v", want, pt.Value)
		}
	}
}

func TestBuildVMPoints_StateRunning(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	p := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		moduleLogger: logger.NewModuleLogger(baseLogger, "probe.hyperv"),
	}
	p.SetProbeType(ProbeType)

	vms := []msvmComputerSystem{{Name: "vm1", EnabledState: enabledStateRunning}}
	points := p.buildVMPoints(vms, nil, time.Now(), nil)

	for _, pt := range points {
		if pt.Name == "hyperv.vm.state" && pt.Value != 1 {
			t.Errorf("running VM should have state=1, got %v", pt.Value)
		}
	}
}

func TestBuildVMPoints_CountBuckets(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
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
	points := p.buildVMPoints(vms, nil, time.Now(), nil)

	counts := map[string]float64{}
	for _, pt := range points {
		if pt.Name == "hyperv.vm.count" {
			for _, tg := range pt.Tags {
				if tg.Key == "state" {
					counts[tg.Value] = pt.Value
				}
			}
		}
	}
	cases := map[string]float64{"running": 1, "stopped": 1, "paused": 1}
	for state, want := range cases {
		if got := counts[state]; got != want {
			t.Errorf("count[%s]: expected %v, got %v", state, want, got)
		}
	}
}

// Property lists of the two classes, as Get-CimClass reports them on a
// Windows Server 2025 Hyper-V host. A selected name outside them turns
// the whole WMI query into "Invalid query", which is how the probe
// collected nothing: it asked Msvm_ComputerSystem for NumberOfProcessors
// and Msvm_SummaryInformation for CPUUsage.
var (
	computerSystemSchema = "Caption,Description,ElementName,InstanceID,CommunicationStatus,DetailedStatus,HealthState,InstallDate,Name,OperatingStatus,OperationalStatus,PrimaryStatus,Status,StatusDescriptions,AvailableRequestedStates,EnabledDefault,EnabledState,OtherEnabledState,RequestedState,TimeOfLastStateChange,TransitioningToState,CreationClassName,IdentifyingDescriptions,NameFormat,OtherIdentifyingInfo,PrimaryOwnerContact,PrimaryOwnerName,Roles,Dedicated,OtherDedicatedDescriptions,PowerManagementCapabilities,ResetCapability,EnhancedSessionModeState,FailedOverReplicationType,HwThreadsPerCoreRealized,LastApplicationConsistentReplicationTime,LastReplicationTime,LastReplicationType,LastSuccessfulBackupTime,ManagementVtlImageFileName,ManagementVtlImageVersion,NumberOfNumaNodes,OnTimeInMilliseconds,ProcessID,ReplicationHealth,ReplicationMode,ReplicationState,TimeOfLastConfigurationChange"
	summarySchema        = "Caption,Description,ElementName,InstanceID,CreationTime,EnabledState,EnhancedSessionModeState,HealthState,HostComputerSystemName,Name,Notes,NumberOfProcessors,OperationalStatus,OtherEnabledState,StatusDescriptions,UpTime,Version,VirtualSwitchNames,VirtualSystemSubType,AllocatedGPU,ApplicationHealth,AsynchronousTasks,AvailableMemoryBuffer,GuestOperatingSystem,Heartbeat,HypervisorPartitionId,IntegrationServicesVersionState,MemoryAvailable,MemorySpansPhysicalNumaNodes,MemoryUsage,ProcessorLoad,ProcessorLoadHistory,ReplicationHealth,ReplicationHealthEx,ReplicationMode,ReplicationProviderId,ReplicationState,ReplicationStateEx,Shielded,Snapshots,SwapFilesInUse,TestReplicaSystem,ThumbnailImage,ThumbnailImageHeight,ThumbnailImageWidth"
)

func TestSelectedPropertiesExistOnTheirClass(t *testing.T) {
	for _, c := range []struct {
		class  string
		schema string
		props  []string
	}{
		{"Msvm_ComputerSystem", computerSystemSchema, computerSystemProps},
		{"Msvm_SummaryInformation", summarySchema, summaryProps},
	} {
		known := map[string]bool{}
		for _, p := range strings.Split(c.schema, ",") {
			known[p] = true
		}
		for _, p := range c.props {
			if !known[p] {
				t.Errorf("%s has no property %q: WMI would reject the whole query", c.class, p)
			}
		}
	}
}

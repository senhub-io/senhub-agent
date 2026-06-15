//go:build windows

// Package hyperv implements the free hyper-v probe: WMI-based monitoring of
// Hyper-V virtual machines on a Windows host. Requires the agent to run with
// administrator privileges (the Hyper-V WMI namespace is protected).
//
// The probe queries root\virtualization\v2:
//   - Msvm_ComputerSystem (EnabledState, NumberOfProcessors)
//   - Msvm_SummaryInformation (CPUUsage, MemoryUsage, UpTime)
//   - Msvm_KvpExchangeComponentSettingData (guest machine-id via KVP)
//
// Metrics emitted (OTel-first):
//   - senhub.hyperv.up           gauge{1}    — probe health (WMI reachable)
//   - hyperv.vm.cpu.usage        gauge{1}    — per-VM CPU fraction (0–1)
//   - hyperv.vm.memory.usage     gauge{By}   — per-VM memory in bytes
//   - hyperv.vm.state            gauge{1}    — 1=running, 0=other
//   - hyperv.vm.count            gauge{1}    — VM count by state
//
// Entities emitted (ADR 0018 — host.id is always a machine-id):
//   - compute.vm per VM (id={host.id:<hypervisor machine-id>, vmid:<GUID>})
//   - host per VM when the guest machine-id is available via KVP
//   - runs_on(compute.vm → host) and monitors(agent → compute.vm) edges
package hyperv

import (
	"context"
	"fmt"
	"time"

	"github.com/yusufpapurcu/wmi"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the canonical registry / license / transformer name.
const ProbeType = "hyperv"

const hypervNamespace = `root\virtualization\v2`

// Hyper-V EnabledState values (Msvm_ComputerSystem.EnabledState).
const (
	enabledStateRunning  = 2
	enabledStateStopped  = 3
	enabledStateEnabled  = 6 // alias seen in some Hyper-V versions for paused
	enabledStateStarting = 9
	enabledStateReset    = 10
	enabledStateSaving   = 11
	enabledStatePausing  = 15
	enabledStateResuming = 17
	enabledStatePaused   = 32768
)

// msvmComputerSystem is the WMI projection of Msvm_ComputerSystem.
type msvmComputerSystem struct {
	Name               string
	EnabledState       uint16
	NumberOfProcessors uint16
}

// msvmSummaryInformation is the WMI projection of Msvm_SummaryInformation.
type msvmSummaryInformation struct {
	Name        string
	ElementName string
	CPUUsage    uint32
	MemoryUsage uint64
	UpTime      uint64
}

// wmiQueryFn is the function used to run WMI queries; replaceable in tests.
type wmiQueryFn func(query string, dst interface{}, namespace string) error

// HypervProbe monitors Hyper-V VMs on the local Windows host via WMI.
type HypervProbe struct {
	*types.BaseProbe
	config       probeConfig
	moduleLogger *logger.ModuleLogger
	queryFn      wmiQueryFn

	entitySource           *hypervEntitySource
	unregisterEntitySource func()
}

type probeConfig struct {
	Interval time.Duration
}

const defaultInterval = 60 * time.Second

// NewHypervProbe constructs the probe. Returns an error only on invalid config.
func NewHypervProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe."+ProbeType)

	cfg := probeConfig{Interval: defaultInterval}
	if v, ok := config["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	// Resolve the hypervisor's own machine-id for the entity source. A failure
	// here is non-fatal: the entity source will return ok=false until the host
	// identity is resolvable (degraded entity correlation, metrics still flow).
	var hostID string
	if ident, err := common.GetHostIdentity(); err != nil {
		moduleLogger.Warn().Err(err).Msg("could not resolve hypervisor host.id; entity source disabled")
	} else {
		hostID = ident.ID
	}

	probe := &HypervProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
		queryFn:      wmi.QueryNamespace,
		entitySource: newHypervEntitySource(hostID, moduleLogger),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *HypervProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *HypervProbe) ShouldStart() bool          { return true }
func (p *HypervProbe) GetInterval() time.Duration { return p.config.Interval }

func (p *HypervProbe) OnStart(_ chan struct{}) error {
	p.unregisterEntitySource = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().Msg("Starting hyperv probe")
	return nil
}

func (p *HypervProbe) OnShutdown(_ context.Context) error {
	if p.unregisterEntitySource != nil {
		p.unregisterEntitySource()
	}
	return nil
}

// Collect queries Hyper-V WMI classes and returns metric datapoints.
func (p *HypervProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	// host.id (and host.*) on every metric so the VM telemetry joins the
	// hypervisor host entity emitted by the foundation detector — Hyper-V is
	// a host facet, not a separate entity (see #456). Best-effort: a host-tag
	// failure degrades correlation but never blocks collection.
	hostTags, hErr := common.GetHostTags()
	if hErr != nil {
		p.moduleLogger.Warn().Err(hErr).Msg("host tags unavailable; hyperv metrics omit host.id")
		hostTags = nil
	}

	vms, sumByName, err := p.queryWMI()

	up := float64(1)
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("Hyper-V WMI query failed")
		up = 0
	}

	points := []data_store.DataPoint{
		{
			Name:      "senhub.hyperv.up",
			Value:     up,
			Timestamp: now,
			Tags:      withHost(hostTags, tags.Tag{Key: "metric_type", Value: "status"}),
		},
	}

	if err == nil {
		points = append(points, p.buildVMPoints(vms, sumByName, now, hostTags)...)
		// Entity rail: update the cached snapshot for the detector goroutine.
		// nil only when the probe was built outside NewHypervProbe (tests that
		// bypass the constructor); guard so Collect never panics.
		if p.entitySource != nil {
			p.entitySource.update(toVMInfos(vms, sumByName))
		}
	}

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// withHost returns a fresh tag slice = host tags followed by the extras, so
// every datapoint carries host.id without callers mutating a shared slice.
func withHost(hostTags []tags.Tag, extra ...tags.Tag) []tags.Tag {
	out := make([]tags.Tag, 0, len(hostTags)+len(extra))
	out = append(out, hostTags...)
	out = append(out, extra...)
	return out
}

// queryWMI runs both WMI queries.
func (p *HypervProbe) queryWMI() ([]msvmComputerSystem, map[string]msvmSummaryInformation, error) {
	var vms []msvmComputerSystem
	vmQuery := "SELECT Name,EnabledState,NumberOfProcessors FROM Msvm_ComputerSystem WHERE Caption='Virtual Machine'"
	if err := p.queryFn(vmQuery, &vms, hypervNamespace); err != nil {
		return nil, nil, fmt.Errorf("Msvm_ComputerSystem query: %w", err)
	}

	var summaries []msvmSummaryInformation
	sumQuery := "SELECT Name,ElementName,CPUUsage,MemoryUsage,UpTime FROM Msvm_SummaryInformation WHERE ElementName IS NOT NULL"
	if err := p.queryFn(sumQuery, &summaries, hypervNamespace); err != nil {
		return nil, nil, fmt.Errorf("Msvm_SummaryInformation query: %w", err)
	}

	sumByName := make(map[string]msvmSummaryInformation, len(summaries))
	for _, s := range summaries {
		key := s.Name
		if key == "" {
			key = s.ElementName
		}
		sumByName[key] = s
	}
	return vms, sumByName, nil
}

// toVMInfos converts the WMI query results to the platform-neutral vmInfo
// slice consumed by the entity source. ElementName from SummaryInformation is
// the human-readable VM name; the GUID (Msvm_ComputerSystem.Name) is the
// immutable VM identity.
func toVMInfos(vms []msvmComputerSystem, sumByName map[string]msvmSummaryInformation) []vmInfo {
	infos := make([]vmInfo, 0, len(vms))
	for _, vm := range vms {
		name := ""
		if si, ok := sumByName[vm.Name]; ok && si.ElementName != "" {
			name = si.ElementName
		}
		infos = append(infos, vmInfo{
			GUID:   vm.Name,
			VMName: name,
			State:  vmStateName(vm.EnabledState),
		})
	}
	return infos
}

// vmStateName converts a Hyper-V EnabledState to the OTel-normalised string
// used as the vm.state attribute on the compute.vm entity.
func vmStateName(state uint16) string {
	switch state {
	case enabledStateRunning:
		return "running"
	case enabledStateStopped:
		return "stopped"
	case enabledStatePaused, enabledStatePausing:
		return "paused"
	case enabledStateSaving:
		return "saving"
	case enabledStateStarting, enabledStateResuming:
		return "starting"
	case enabledStateReset:
		return "resetting"
	default:
		return "unknown"
	}
}

// buildVMPoints builds per-VM and per-state-count datapoints.
func (p *HypervProbe) buildVMPoints(vms []msvmComputerSystem, sumByName map[string]msvmSummaryInformation, ts time.Time, hostTags []tags.Tag) []data_store.DataPoint {
	var points []data_store.DataPoint

	running, stopped, paused := 0, 0, 0

	for _, vm := range vms {
		name := vm.Name
		// Prefer the human-friendly ElementName from SummaryInformation.
		if si, ok := sumByName[name]; ok && si.ElementName != "" {
			name = si.ElementName
		}

		vmTags := withHost(hostTags,
			tags.Tag{Key: "hyperv.vm.name", Value: name},
			tags.Tag{Key: "metric_type", Value: "vm"},
		)

		// hyperv.vm.state — 1 when the VM is running, 0 otherwise.
		stateVal := float64(0)
		if vm.EnabledState == enabledStateRunning {
			stateVal = 1
		}
		points = append(points, data_store.DataPoint{
			Name: "hyperv.vm.state", Value: stateVal, Timestamp: ts, Tags: vmTags,
		})

		switch vm.EnabledState {
		case enabledStateRunning, enabledStateEnabled:
			running++
		case enabledStatePaused, enabledStatePausing, enabledStateSaving:
			paused++
		default:
			stopped++
		}

		if si, ok := sumByName[vm.Name]; ok {
			// hyperv.vm.cpu.usage — CPUUsage is a percentage (0–100), normalise to 0–1.
			points = append(points, data_store.DataPoint{
				Name:      "hyperv.vm.cpu.usage",
				Value:     float64(si.CPUUsage) / 100.0,
				Timestamp: ts,
				Tags:      vmTags,
			})
			// hyperv.vm.memory.usage — MemoryUsage is in MB, convert to bytes.
			points = append(points, data_store.DataPoint{
				Name:      "hyperv.vm.memory.usage",
				Value:     float64(si.MemoryUsage) * 1024 * 1024,
				Timestamp: ts,
				Tags:      vmTags,
			})
		}
	}

	// Per-state count metrics (one datapoint per state bucket).
	countMetrics := []struct {
		state string
		count int
	}{
		{"running", running},
		{"stopped", stopped},
		{"paused", paused},
	}
	for _, cm := range countMetrics {
		points = append(points, data_store.DataPoint{
			Name:  "hyperv.vm.count",
			Value: float64(cm.count),
			Tags: withHost(hostTags,
				tags.Tag{Key: "state", Value: cm.state},
				tags.Tag{Key: "metric_type", Value: "vm_count"},
			),
			Timestamp: ts,
		})
	}

	return points
}

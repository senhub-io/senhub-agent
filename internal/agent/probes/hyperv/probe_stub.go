//go:build !windows

// Package hyperv provides a non-Windows stub so the probe compiles and
// registers on every platform. Operators can keep a single configuration
// that works across mixed-OS deployments; instantiation fails loudly with
// a clear message on platforms that lack the Hyper-V WMI namespace.
// The same approach is used by windows_eventlog on non-Windows hosts.
package hyperv

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

const ProbeType = "hyperv"

const defaultInterval = 60 * time.Second

// HypervProbe is the non-Windows stub. It satisfies types.Probe so the probe
// registers everywhere; OnStart returns a clear error on non-Windows hosts.
type HypervProbe struct {
	*types.BaseProbe
	interval time.Duration
}

func NewHypervProbe(config map[string]interface{}, _ *logger.Logger) (types.Probe, error) {
	interval := defaultInterval
	if v, ok := config["interval"].(int); ok && v > 0 {
		interval = time.Duration(v) * time.Second
	}
	probe := &HypervProbe{
		BaseProbe: &types.BaseProbe{},
		interval:  interval,
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *HypervProbe) GetTargetStrategies() []string         { return []string{} }
func (p *HypervProbe) ShouldStart() bool                     { return true }
func (p *HypervProbe) GetInterval() time.Duration            { return p.interval }
func (p *HypervProbe) Collect() ([]data_store.DataPoint, error) { return nil, nil }

func (p *HypervProbe) OnStart(_ chan struct{}) error {
	return fmt.Errorf("hyperv probe is not supported on %s (requires the Hyper-V WMI namespace available only on Windows Server)", runtime.GOOS)
}

func (p *HypervProbe) OnShutdown(_ context.Context) error { return nil }

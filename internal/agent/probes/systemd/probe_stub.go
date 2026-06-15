//go:build !linux

// Package systemd — non-Linux stub. Systemd is a Linux-only init
// system; the D-Bus API used by this probe is not available on Darwin
// or Windows. The probe registers on all platforms so a single config
// works in mixed-OS deployments; it returns ErrNotSupported on Start,
// which the poller logs once and then treats as inert.
package systemd

import (
	"context"
	"errors"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// ProbeType is the canonical type name (same on all platforms).
const ProbeType = "systemd"

// ErrNotSupported is returned on non-Linux platforms.
var ErrNotSupported = errors.New("systemd probe is only supported on Linux")

// SystemdProbe is the stub implementation.
type SystemdProbe struct {
	*types.BaseProbe
}

// NewSystemdProbe returns a stub that will refuse to start.
func NewSystemdProbe(_ map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	probe := &SystemdProbe{BaseProbe: &types.BaseProbe{}}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func (p *SystemdProbe) GetTargetStrategies() []string      { return nil }
func (p *SystemdProbe) ShouldStart() bool                  { return false }
func (p *SystemdProbe) GetInterval() time.Duration         { return 30 * time.Second }
func (p *SystemdProbe) OnShutdown(_ context.Context) error { return nil }

func (p *SystemdProbe) OnStart(_ chan struct{}) error {
	return ErrNotSupported
}

func (p *SystemdProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, ErrNotSupported
}

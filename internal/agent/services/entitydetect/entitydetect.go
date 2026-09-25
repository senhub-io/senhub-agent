// Package entitydetect runs the producer half of the entity rail: it
// polls the registered sources and publishes the events that describe
// what this host is and what it runs.
//
// It exists as a service of its own because the producer was built
// inside the OTLP strategy, which made the whole rail conditional on one
// output being configured. An agent deployed for Zabbix, PRTG or Nagios
// registered entity sources from its probes that nothing ever polled, so
// the question "can this output carry entities" could not even be asked
// — the events were never produced (#932).
//
// The consumer half stays where it belongs: each output subscribes to
// entity.SubscribeEvents and encodes what it can use. The channel fans
// out, so several outputs can read the same events.
package entitydetect

import (
	"context"
	"net"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/entity/hostdep"
	"senhub-agent.go/internal/agent/services/entity/hostiface"
	"senhub-agent.go/internal/agent/services/entity/hostnet"
	"senhub-agent.go/internal/agent/services/entity/hostsvc"
	"senhub-agent.go/internal/agent/services/governance"
	"senhub-agent.go/internal/agent/services/logger"
)

// Config is what the producer needs. It carries nothing about how the
// events are encoded or shipped: that belongs to whichever output
// consumes them.
type Config struct {
	// Enabled starts the detector. Off means the sources probes register
	// are never polled and no event is produced.
	Enabled bool

	// Interval is the heartbeat cadence. Every entity and relation is
	// re-emitted each interval, and the interval travels on each event as
	// the consumer's liveness backstop.
	Interval time.Duration

	// AgentInstanceID is the agent's own service.instance.id, which the
	// probe sources stamp on the `monitors` edge so it points at the same
	// node the foundation describes.
	AgentInstanceID string
	AgentService    string
	AgentVersion    string

	// Environment is stamped on the host entity.
	Environment string
	// Governance is the operator metadata for this host
	// (owner, criticality, location, lifecycle).
	Governance governance.Governance

	// DependsOnEnabled gates the outbound dependency source. Off by
	// default: mapping a host's outbound connections can be
	// privacy-sensitive (#213).
	DependsOnEnabled      bool
	DependsOnDebounce     int
	DependsOnExcludeCIDRs []*net.IPNet
}

// Service is the lifecycle wrapper around the detector.
type Service struct {
	cfg    Config
	logger *logger.ModuleLogger

	cancel      context.CancelFunc
	unregisters []func()
}

// New builds the service. It does not start anything.
func New(cfg Config, base *logger.Logger) *Service {
	return &Service{cfg: cfg, logger: logger.NewModuleLogger(base, "entity.detector")}
}

func (s *Service) GetName() string { return "EntityDetector" }

// Start registers the host-side sources and runs the detector until ctx
// is cancelled. With Enabled false it returns having done nothing, so an
// agent that does not want the cost pays none of it.
func (s *Service) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}

	var warnDegenerate sync.Once
	hostFn := func() (entity.HostIdentity, error) {
		hi, err := common.GetHostIdentity()
		if err != nil {
			return entity.HostIdentity{}, err
		}
		if common.DegenerateHostID(hi.ID) {
			warnDegenerate.Do(func() {
				s.logger.Error().
					Str("host_id", hi.ID).
					Msg("This host's identity looks like an example or a blank value; every host carrying it merges into one on the topology graph. Set a real SENHUB_HOST_ID or fix the machine's DMI/machine-id")
			})
		}
		idSource := ""
		if common.HostIDFromConfiguration(hi.ID) {
			idSource = "configuration"
		}
		return entity.HostIdentity{
			ID:                    hi.ID,
			IDSource:              idSource,
			Name:                  hi.Name,
			OSType:                hi.OSType,
			Arch:                  hi.Arch,
			OSName:                hi.OSName,
			OSVersion:             hi.OSVersion,
			OSBuildID:             hi.OSBuildID,
			OSDescription:         hi.OSDescription,
			CPUModel:              hi.CPUModel,
			CPUVendor:             hi.CPUVendor,
			HWVendor:              hi.HWVendor,
			HWModel:               hi.HWModel,
			HWSerial:              hi.HWSerial,
			CPULogicalCount:       hi.CPULogicalCount,
			CPUPhysicalCount:      hi.CPUPhysicalCount,
			CPUFreqHz:             hi.CPUFreqHz,
			MemTotal:              hi.MemTotal,
			DiskTotal:             hi.DiskTotal,
			Virtualization:        hi.Virtualization,
			ChassisType:           hi.ChassisType,
			CloudProvider:         hi.CloudProvider,
			CloudRegion:           hi.CloudRegion,
			CloudAvailabilityZone: hi.CloudAvailabilityZone,
			CloudAccountID:        hi.CloudAccountID,
			HostType:              hi.HostType,
			ContainerRuntime:      hi.ContainerRuntime,
			K8sNodeName:           hi.K8sNodeName,
			Environment:           s.cfg.Environment,
			Governance:            s.cfg.Governance.Attributes(),
		}, nil
	}
	agentFn := func() entity.AgentIdentity {
		return entity.AgentIdentity{
			InstanceID:     s.cfg.AgentInstanceID,
			ServiceName:    s.cfg.AgentService,
			ServiceVersion: s.cfg.AgentVersion,
		}
	}

	// Probe entity sources stamp the From endpoint of their `monitors`
	// edge with this, so it must be set before the detector polls them.
	agentstate.SetAgentInstanceID(s.cfg.AgentInstanceID)

	hostIDFn := func() string {
		hi, err := common.GetHostIdentity()
		if err != nil {
			return ""
		}
		return hi.ID
	}
	s.unregisters = append(s.unregisters,
		entity.RegisterSource(hostnet.New(hostIDFn)),
		entity.RegisterSource(hostsvc.New(hostIDFn)),
		entity.RegisterSource(hostiface.New(hostIDFn)))

	if s.cfg.DependsOnEnabled {
		dep := hostdep.New(hostIDFn, s.cfg.DependsOnDebounce, s.cfg.DependsOnExcludeCIDRs)
		// Mapping a socket to its owning process reads /proc/<pid>/fd,
		// which is owner-only: a non-root daemon sees every other
		// service's connections with no owner and can emit nothing for
		// them (#808). Say so once, with the counts, rather than let an
		// empty rail read as "this host depends on nothing".
		dep.OnBlind(func(observed, unattributable int) {
			s.logger.Warn().
				Int("outbound_sockets", observed).
				Int("unattributable", unattributable).
				Msg("outbound dependencies could not be attributed to a process; the agent may lack the privilege to read them")
		})
		s.unregisters = append(s.unregisters, entity.RegisterSource(dep))
	}

	det := entity.NewDetector(hostFn, agentFn, s.cfg.Interval)
	det.OnOrphanRelations(func(orphans []entity.Relation) {
		for _, r := range orphans {
			s.logger.Warn().
				Str("relation", r.Type).
				Str("from_type", r.FromType).
				Str("to_type", r.ToType).
				Msg("entity relation has no source entity this cycle; dropped before emission")
		}
	})
	det.OnOrphanEntities(func(orphans []entity.Entity) {
		for _, e := range orphans {
			s.logger.Warn().
				Str("entity_type", e.Type).
				Interface("entity_id", e.ID).
				Msg("entity has no relation; dropped before emission (anti-orphan guard)")
		}
	})

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go det.Run(runCtx)

	s.logger.Info().
		Dur("interval", s.cfg.Interval).
		Bool("depends_on", s.cfg.DependsOnEnabled).
		Msg("entity detection started")
	return nil
}

// Shutdown stops the detector and releases the host sources, so a
// restart does not register them twice.
func (s *Service) Shutdown(context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	for _, unregister := range s.unregisters {
		unregister()
	}
	s.unregisters = nil
	return nil
}

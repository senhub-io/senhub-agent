// internal/agent/probes/cpu/cpuProbe.go
package cpu

import (
	"time"

	"github.com/shirou/gopsutil/v3/host"

	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// NewCpuProbe crée une nouvelle instance de CPU probe. Le cycle de vie
// (intervalle, enrichissement, arrêt) vient de hostpoll ; seule la
// collecte OS appartient à ce paquet.
func NewCpuProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	probe, err := hostpoll.New(config, baseLogger, hostpoll.Spec{
		Module:   "probe.cpu",
		Subject:  "CPU",
		TypeName: "CPUProbe",
		NewCollector: func(cfg map[string]interface{}, _ *logger.Logger, moduleLogger *logger.ModuleLogger) (hostpoll.Collector, error) {
			collector, err := newCPUCollector(cfg, moduleLogger.Logger)
			if err != nil {
				return nil, err
			}
			return withClock{
				Collector: collector,
				now:       time.Now,
				uptime:    host.Uptime,
				onUptimeError: func(err error) {
					moduleLogger.Warn().Err(err).Msg("reading the host uptime failed; the uptime point is left out of this cycle")
				},
			}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return probe, nil
}

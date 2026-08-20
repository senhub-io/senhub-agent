// internal/agent/probes/cpu/cpuProbe.go
package cpu

import (
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
			return newCPUCollector(cfg, moduleLogger.Logger)
		},
	})
	if err != nil {
		return nil, err
	}
	return probe, nil
}

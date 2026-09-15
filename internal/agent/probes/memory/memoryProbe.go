// internal/agent/probes/memory/memoryProbe.go
package memory

import (
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// NewMemoryProbe crée une nouvelle instance de Memory probe. Le cycle de
// vie vient de hostpoll ; seule la collecte OS appartient à ce paquet.
func NewMemoryProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	probe, err := hostpoll.New(config, baseLogger, hostpoll.Spec{
		Module:   "probe.memory",
		Subject:  "Memory",
		TypeName: "MemoryProbe",
		NewCollector: func(cfg map[string]interface{}, base *logger.Logger, _ *logger.ModuleLogger) (hostpoll.Collector, error) {
			return newMemoryCollector(cfg, base)
		},
	})
	if err != nil {
		return nil, err
	}
	return probe, nil
}

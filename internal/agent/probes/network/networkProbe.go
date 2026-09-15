// internal/agent/probes/network/networkProbe.go
package network

import (
	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// interfaceNameTag is the identity key of the network.interface entity, and
// therefore the tag a consumer joins a host-interface series to its entity
// with. It must stay byte-identical to the value the entity source puts in
// entity.ID (hostiface: net.Interface.Name), and it must not be renamed by a
// transformer — a subject key that survives only under another spelling
// joins nothing, which is what #748 was.
//
// It is spelled the same as the SNMP side (snmppoll stamps interface.name on
// every polled-device interface metric), so one query shape reaches both
// planes.
const interfaceNameTag = "interface.name"

// NewNetworkProbe crée une nouvelle instance de Network probe. Le cycle
// de vie vient de hostpoll ; seule la collecte OS appartient à ce paquet.
func NewNetworkProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	probe, err := hostpoll.New(config, baseLogger, hostpoll.Spec{
		Module:   "probe.network",
		Subject:  "network",
		TypeName: "NetworkProbe",
		NewCollector: func(cfg map[string]interface{}, base *logger.Logger, _ *logger.ModuleLogger) (hostpoll.Collector, error) {
			return newNetworkCollector(cfg, base)
		},
	})
	if err != nil {
		return nil, err
	}
	return probe, nil
}

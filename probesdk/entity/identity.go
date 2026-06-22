package entity

import (
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
)

// AgentInstanceID returns the agent's own service.instance.id — the From
// endpoint of a `monitors` edge from the agent to a target it watches. It is
// "" when entity emission is off; the consumer would buffer then drop an edge
// whose From cannot be resolved, so a probe emits the monitors edge only when
// this is non-empty:
//
//	if agentID := entity.AgentInstanceID(); agentID != "" {
//		obs.Relations = append(obs.Relations, entity.Relation{
//			Type:     "monitors",
//			FromType: "service.instance", FromID: map[string]any{"service.instance.id": agentID},
//			ToType:   "service.instance", ToID: targetID,
//		})
//	}
//
// SDK probes (free-tier here and paid in the separate enterprise module) use
// this instead of the internal agentstate package, which Go forbids importing
// across the module boundary.
func AgentInstanceID() string {
	return agentstate.GetAgentInstanceID()
}

// HostID returns the agent host's stable machine-id — the same id the host
// entity uses, so a `runs_on`→host edge from a target on the agent's own host
// resolves. It is "" when the host identity cannot be read; emit the runs_on
// edge only when this is non-empty.
func HostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

package entity

import (
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	ientity "senhub-agent.go/internal/agent/services/entity"
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

// LocalRunsOn returns a `<fromType> --runs_on--> host` relation when the
// monitored target is on the agent's own host (serverAddress is loopback,
// "localhost" or empty), so a locally-monitored service hangs off the host node
// instead of floating with only its `monitors` anchor. ok=false for a remote or
// unknown address, or when hostID is "". A probe that already mints its identity
// as "<svc>@<host>" passes its service.instance.id as fromID:
//
//	if rel, ok := entity.LocalRunsOn("service.instance", svcID, serverAddr, hostID); ok {
//		obs.Relations = append(obs.Relations, rel)
//	}
func LocalRunsOn(fromType string, fromID map[string]any, serverAddress, hostID string) (Relation, bool) {
	return ientity.LocalRunsOn(fromType, fromID, serverAddress, hostID)
}

// IsLoopbackHost reports whether serverAddress denotes the agent's own host with
// certainty (empty, "localhost", or a loopback IP). Exposed for probes that need
// the predicate without building a relation.
func IsLoopbackHost(serverAddress string) bool {
	return ientity.IsLoopbackHost(serverAddress)
}

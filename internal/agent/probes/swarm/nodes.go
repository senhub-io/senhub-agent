package swarm

import (
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Nodes and quorum.
//
// A Swarm that has lost manager quorum is the failure worth catching, and it is
// invisible from any single host: each machine keeps running its containers
// while the cluster silently refuses every change — no deploy, no rescheduling,
// no scaling. From inside one node everything looks fine.

var nodeStates = []string{"ready", "down", "unknown", "disconnected"}
var nodeAvailabilities = []string{"active", "pause", "drain"}
var nodeReachabilities = []string{"reachable", "unreachable", "unknown"}

// nodePoints emits per-node condition plus the cluster-level quorum verdict.
func (p *swarmProbe) nodePoints(nodes []node, clusterTags []tags.Tag, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	managers, reachableManagers, workers := 0, 0, 0

	for i := range nodes {
		n := &nodes[i]
		base := withTags(clusterTags,
			tags.Tag{Key: "swarm.node.id", Value: n.ID},
			tags.Tag{Key: "swarm.node.name", Value: n.Description.Hostname},
			tags.Tag{Key: "swarm.node.role", Value: n.Spec.Role},
			tags.Tag{Key: "metric_type", Value: "node"},
		)

		ready := float64(0)
		if strings.EqualFold(n.Status.State, "ready") {
			ready = 1
		}
		points = append(points, point("swarm.node.ready", ready, now, base))
		points = append(points, oneHot("swarm.node.state", n.Status.State, nodeStates, now, base, "state")...)

		// Availability is the operator's intent, state is the reality. A node
		// drained on purpose and a node that fell over both stop taking work,
		// and only these two series together tell them apart.
		points = append(points, oneHot("swarm.node.availability", n.Spec.Availability,
			nodeAvailabilities, now, base, "availability")...)

		if n.Description.Resources.NanoCPUs > 0 {
			points = append(points, point("swarm.node.cpu.allocatable",
				float64(n.Description.Resources.NanoCPUs)/1e9, now, base))
		}
		if n.Description.Resources.MemoryBytes > 0 {
			points = append(points, point("swarm.node.memory.allocatable",
				float64(n.Description.Resources.MemoryBytes), now, base))
		}

		if strings.EqualFold(n.Spec.Role, "manager") {
			managers++
			leader, reachable := float64(0), float64(0)
			reachability := "unknown"
			if n.ManagerStatus != nil {
				if n.ManagerStatus.Leader {
					leader = 1
				}
				reachability = n.ManagerStatus.Reachability
				if strings.EqualFold(reachability, "reachable") {
					reachable = 1
					reachableManagers++
				}
			}
			points = append(points,
				point("swarm.node.manager.leader", leader, now, base),
				point("swarm.node.manager.reachable", reachable, now, base))
			points = append(points, oneHot("swarm.node.manager.reachability", reachability,
				nodeReachabilities, now, base, "reachability")...)
		} else {
			workers++
		}
	}

	clusterBase := withTags(clusterTags, tags.Tag{Key: "metric_type", Value: "cluster"})
	points = append(points,
		point("swarm.cluster.nodes", float64(len(nodes)), now, clusterBase),
		point("swarm.cluster.managers", float64(managers), now, clusterBase),
		point("swarm.cluster.managers.reachable", float64(reachableManagers), now, clusterBase),
		point("swarm.cluster.workers", float64(workers), now, clusterBase),
		point("swarm.cluster.quorum", boolValue(hasQuorum(managers, reachableManagers)), now, clusterBase),
	)
	return points
}

// hasQuorum reports whether a strict majority of managers is reachable, which
// is Raft's condition for the cluster to accept any change.
//
// Emitted as its own series rather than left for a dashboard expression: the
// rule is "strictly more than half", and every place that re-derives it from
// two counters is a place to get the boundary wrong. Two managers with one
// reachable is NOT quorum — the most common mistake, and the exact shape of a
// two-manager cluster that has lost one.
func hasQuorum(managers, reachable int) bool {
	if managers == 0 {
		return false
	}
	return reachable*2 > managers
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

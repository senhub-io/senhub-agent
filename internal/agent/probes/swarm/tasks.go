package swarm

import (
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Tasks: where a deploy actually fails.
//
// Swarm's task states are a lifecycle, not a health flag, and the distinction
// an operator needs is between "not there yet" (new, pending, assigned,
// preparing, starting) and "will never get there" (rejected, failed, orphaned).
// A service stuck at 2/3 replicas reads identically in both cases from the
// service object alone; only the task states say which.

// taskStates is the full lifecycle as defined by the Engine, in order.
var taskStates = []string{
	"new", "allocated", "pending", "assigned", "accepted",
	"preparing", "ready", "starting", "running",
	"complete", "shutdown", "failed", "rejected", "remove", "orphaned",
}

// terminalFailureStates are the states from which a task never recovers. They
// are counted separately because they are the ones worth waking someone for.
var terminalFailureStates = map[string]bool{
	"failed":   true,
	"rejected": true,
	"orphaned": true,
}

// taskPoints emits per-service task counts by state plus a failure count.
//
// Counts per (service, state) rather than one series per task: a task id is
// regenerated on every restart, so a per-task series would create unbounded
// cardinality and every series would be a one-sample dead end.
func (p *swarmProbe) taskPoints(services []service, allTasks []task, clusterTags []tags.Tag, now time.Time) []data_store.DataPoint {
	names := serviceNames(services)

	type key struct{ serviceID, state string }
	counts := map[key]int{}
	failures := map[string]int{}
	slotsByNode := map[string]int{}

	for i := range allTasks {
		t := &allTasks[i]
		state := strings.ToLower(t.Status.State)
		counts[key{t.ServiceID, state}]++
		if terminalFailureStates[state] {
			failures[t.ServiceID]++
		}
		if strings.EqualFold(state, "running") && t.NodeID != "" {
			slotsByNode[t.NodeID]++
		}
	}

	var points []data_store.DataPoint
	for i := range services {
		s := &services[i]
		base := withTags(clusterTags,
			tags.Tag{Key: "swarm.service.id", Value: s.ID},
			tags.Tag{Key: "swarm.service.name", Value: s.Spec.Name},
			tags.Tag{Key: "metric_type", Value: "task"},
		)
		for _, state := range taskStates {
			points = append(points, point("swarm.task.state",
				float64(counts[key{s.ID, state}]), now,
				withTags(base, tags.Tag{Key: "state", Value: state})))
		}
		points = append(points, point("swarm.service.tasks.failed",
			float64(failures[s.ID]), now, base))
	}

	// Running tasks per node: the placement view. A node holding none while
	// its peers hold many is either draining or refusing work, and the node
	// metrics alone do not say which.
	for nodeID, n := range slotsByNode {
		points = append(points, point("swarm.node.tasks.running", float64(n), now,
			withTags(clusterTags,
				tags.Tag{Key: "swarm.node.id", Value: nodeID},
				tags.Tag{Key: "metric_type", Value: "task"})))
	}

	// Tasks whose service no longer exists. Swarm leaves these behind on a
	// removed service until the reaper runs, and they are invisible from every
	// per-service view precisely because there is no service to look under.
	orphaned := 0
	for i := range allTasks {
		if _, ok := names[allTasks[i].ServiceID]; !ok {
			orphaned++
		}
	}
	points = append(points, point("swarm.cluster.tasks.orphaned", float64(orphaned), now,
		withTags(clusterTags, tags.Tag{Key: "metric_type", Value: "task"})))

	return points
}

func serviceNames(services []service) map[string]string {
	out := make(map[string]string, len(services))
	for i := range services {
		out[services[i].ID] = services[i].Spec.Name
	}
	return out
}

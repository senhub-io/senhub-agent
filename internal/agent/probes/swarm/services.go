package swarm

import (
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Services: what was asked for, and what is actually running.
//
// The gap between the two is the number an operator watches during a deploy,
// and it is the one Swarm makes hardest to see — `docker service ls` prints
// "3/5" as a string in a terminal nobody is watching at 3 a.m.

var updateStates = []string{"updating", "paused", "completed", "rollback_started", "rollback_paused", "rollback_completed"}

// servicePoints emits declared vs running replicas, mode, and update state.
//
// Running replicas are counted from the tasks rather than read from the
// service: Swarm does not publish a running count on the service object, and
// deriving it from tasks is also the only figure that reflects reality —
// a service can declare five replicas while five tasks sit in `rejected`.
func (p *swarmProbe) servicePoints(services []service, allTasks []task, clusterTags []tags.Tag, now time.Time) []data_store.DataPoint {
	running := runningTasksByService(allTasks)

	var points []data_store.DataPoint
	for i := range services {
		s := &services[i]
		mode, desired := serviceMode(s, allTasks)

		base := withTags(clusterTags,
			tags.Tag{Key: "swarm.service.id", Value: s.ID},
			tags.Tag{Key: "swarm.service.name", Value: s.Spec.Name},
			tags.Tag{Key: "swarm.service.mode", Value: mode},
			tags.Tag{Key: "metric_type", Value: "service"},
		)

		points = append(points,
			point("swarm.service.replicas.desired", float64(desired), now, base),
			point("swarm.service.replicas.running", float64(running[s.ID]), now, base),
			point("swarm.service.converged", boolValue(int64(running[s.ID]) >= desired), now, base),
		)

		state := ""
		if s.UpdateStatus != nil {
			state = s.UpdateStatus.State
		}
		points = append(points, oneHot("swarm.service.update.state", state, updateStates, now, base, "state")...)

		points = append(points, p.publishedPortPoints(s, base, now)...)
	}
	return points
}

// publishedPortPoints emits one series per published port.
//
// This is the cluster's ingress surface: every port here is reachable on EVERY
// node through the routing mesh, not only on the nodes running the service.
// That is the part of Swarm that surprises people — a port published by a
// service with one replica answers on all twenty machines — so the publish
// mode rides as a label rather than being flattened away.
func (p *swarmProbe) publishedPortPoints(s *service, base []tags.Tag, now time.Time) []data_store.DataPoint {
	ports := s.Endpoint.Ports
	if len(ports) == 0 {
		ports = s.Endpoint.Spec.Ports
	}
	var points []data_store.DataPoint
	for _, pc := range ports {
		if pc.PublishedPort == 0 {
			continue
		}
		t := withTags(base,
			tags.Tag{Key: "swarm.port.published", Value: strconv.FormatInt(pc.PublishedPort, 10)},
			tags.Tag{Key: "swarm.port.target", Value: strconv.FormatInt(pc.TargetPort, 10)},
			tags.Tag{Key: "network.transport", Value: strings.ToLower(pc.Protocol)},
			tags.Tag{Key: "swarm.port.mode", Value: publishMode(pc.PublishMode)},
		)
		points = append(points, point("swarm.service.port.published", 1, now, t))
	}
	return points
}

// publishMode defaults to ingress, which is what the Engine applies when the
// field is absent — reporting "" would read as a third, non-existent mode.
func publishMode(m string) string {
	if strings.TrimSpace(m) == "" {
		return "ingress"
	}
	return strings.ToLower(m)
}

// serviceMode returns the mode and the desired replica count.
//
// A global service has no declared number: it wants one task per eligible node,
// so the desired count is only knowable from the tasks Swarm actually created.
// Reporting 0 for a global service — the obvious reading of a missing field —
// would make every global service look permanently unconverged.
func serviceMode(s *service, allTasks []task) (mode string, desired int64) {
	if s.Spec.Mode.Replicated != nil {
		if r := s.Spec.Mode.Replicated.Replicas; r != nil {
			return "replicated", *r
		}
		return "replicated", 0
	}
	if s.Spec.Mode.Global != nil {
		return "global", int64(desiredTasksForService(s.ID, allTasks))
	}
	return "unknown", 0
}

// desiredTasksForService counts tasks Swarm intends to run — the ones whose
// desired state is running, regardless of whether they got there.
func desiredTasksForService(serviceID string, allTasks []task) int {
	n := 0
	for i := range allTasks {
		if allTasks[i].ServiceID == serviceID && strings.EqualFold(allTasks[i].DesiredState, "running") {
			n++
		}
	}
	return n
}

// runningTasksByService counts tasks actually in the running state.
func runningTasksByService(allTasks []task) map[string]int {
	out := map[string]int{}
	for i := range allTasks {
		if strings.EqualFold(allTasks[i].Status.State, "running") {
			out[allTasks[i].ServiceID]++
		}
	}
	return out
}

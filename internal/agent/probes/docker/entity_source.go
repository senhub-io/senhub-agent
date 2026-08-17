package docker

import (
	"strings"
	"sync"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeContainer  = "container"
	entityTypeHost       = "host"
	idKeyContainerID     = "container.id"
	idKeyHost            = "host.id"
	attrContainerName    = "container.name"
	attrContainerImage   = "container.image.name"
	attrContainerRuntime = "container.runtime"
	attrContainerStatus  = "status"
	attrContainerCreated = "container.created.time"
	relRunsOn            = "runs_on"
	relAttachedTo        = entity.RelAttachedTo
)

// containerStatus normalizes the Docker container State to the canonical
// `status` stateKey value (toise#216 AT5): a change here classifies as an
// entity.state_changed in the consumer (crash, redeploy). running/paused keep
// their name; restarting stays distinct (crash-loop signal); every terminal
// state (exited/created/dead/removing) collapses to stopped. "" → "" (omit).
func containerStatus(state string) string {
	switch strings.ToLower(state) {
	case "":
		return ""
	case "running":
		return "running"
	case "paused":
		return "paused"
	case "restarting":
		return "restarting"
	default:
		return "stopped"
	}
}

// dockerEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The cache is refreshed from the container list
// that Collect() fetches each cycle (no extra API call). ok=false before the
// first successful update so the detector does not treat an empty initial
// cache as "all containers deleted".
type dockerEntitySource struct {
	// hostID resolves the host's stable id so each container is anchored to the
	// host it runs on. nil → defaultHostID (gopsutil machine-id).
	hostID func() string

	mu    sync.Mutex
	cache entity.Observation
	ready bool
}

// defaultHostID returns the host machine-id, or "" when it cannot be read (the
// runs_on edge is then skipped rather than emitted with an unresolvable target).
func defaultHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

// update replaces the entity cache with the current container list.
// Called from Collect() under the probe's own goroutine; must not block.
func (s *dockerEntitySource) update(containers []containerListItem) {
	hostFn := s.hostID
	if hostFn == nil {
		hostFn = defaultHostID
	}
	hostID := hostFn()

	obs := entity.Observation{}
	for _, c := range containers {
		name := primaryName(c)
		cID := map[string]any{idKeyContainerID: c.ID}
		attrs := map[string]any{
			attrContainerName:    name,
			attrContainerImage:   c.Image,
			attrContainerRuntime: "docker",
		}
		// status: the container lifecycle state as a stateKey, so running→stopped
		// is a state change in the consumer, not a silent update (#514, AT5).
		if st := containerStatus(c.State); st != "" {
			attrs[attrContainerStatus] = st
		}
		if c.Created > 0 {
			attrs[attrContainerCreated] = c.Created
		}
		// What the container IS, as its orchestrator states it. A name and an
		// image say how it was started; these say what it belongs to, which is
		// what an operator groups by when something is wrong with "the api
		// service" rather than with one container.
		for k, v := range orchestratorFacts(c.Labels) {
			attrs[k] = v
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entityTypeContainer,
			ID:         cID,
			Attributes: attrs,
		})
		// runs_on container→host: a container is a compute resource that runs on
		// this host. The host node is emitted by the entity foundation, so we
		// only reference it (no re-emit). Without this edge the container floats
		// in the consumer graph (#503). Skipped when host.id is unavailable —
		// the consumer would buffer an unresolvable target then drop the edge.
		if hostID != "" {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relRunsOn,
				FromType: entityTypeContainer,
				FromID:   cID,
				ToType:   entityTypeHost,
				ToID:     map[string]any{idKeyHost: hostID},
			})
		}

		// attached_to container→segment, for the overlay networks this
		// container joined (ADR 0034). The attachment is the only place a
		// segment is observable per workload: the manager knows the segments
		// exist, the nodes know who is on them, so a complete graph needs both
		// probes — which is why the consumer's ADR records the split.
		//
		// Only swarm-scoped overlays produce an edge. A bridge network is
		// local to one engine and identical in name on every host, so an edge
		// to it would either name nothing or false-join hosts through a shared
		// node — the collapse family this project keeps paying for.
		for name, n := range c.NetworkSettings.Networks {
			if !isSwarmOverlayName(name, n.NetworkID) {
				continue
			}
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     relAttachedTo,
				FromType: entityTypeContainer,
				FromID:   cID,
				ToType:   entity.TypeNetworkSegment,
				ToID:     map[string]any{"network.segment.id": "swarm:" + n.NetworkID},
			})
		}
	}

	s.mu.Lock()
	s.cache = obs
	s.ready = true
	s.mu.Unlock()
}

// Observe returns the latest cached entity snapshot. Non-blocking; safe
// to call from the detector goroutine. Returns ok=false until the first
// successful call to update().
func (s *dockerEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}

// isSwarmOverlayName reports whether a container network attachment names a
// swarm-scoped overlay segment.
//
// The container list does not say which driver a network uses, so the decision
// is made on what it does say. A swarm network id is the 25-character
// cluster-assigned string; bridge, host and none are the engine's built-ins and
// are local to one machine.
//
// Conservative on purpose: a missed attachment is a thinner graph, while a
// wrong one draws an edge to a segment that does not exist or, worse, to a node
// shared by every host in the fleet.
func isSwarmOverlayName(name, networkID string) bool {
	switch name {
	case "bridge", "host", "none", "":
		return false
	}
	return len(networkID) == 25
}

// orchestratorFacts extracts the stable facts an orchestrator stamps on a
// container as labels.
//
// A whitelist, not a passthrough: container labels are arbitrary operator
// input, so copying them wholesale would let anyone inflate the graph with
// unbounded keys — and one of them would eventually collide with a real
// attribute name. Only the two orchestrators we support are read, under names
// that say which one spoke.
//
// The Swarm service name is the link this agent could not draw before: the
// swarm probe sees services from the manager, the docker probe sees containers
// on each node, and nothing connected the two. It stays an ATTRIBUTE rather
// than becoming an entity with an edge, because no relation in the vocabulary
// means "belongs to a service": a container does not run ON a service, and
// stretching runs_on to mean it is the overload we refused for network
// segments. Kubernetes is treated the same way here — a Deployment is not an
// entity either, its name rides on what belongs to it.
func orchestratorFacts(labels map[string]string) map[string]any {
	if len(labels) == 0 {
		return nil
	}
	out := map[string]any{}
	for label, attr := range map[string]string{
		"com.docker.swarm.service.name": "swarm.service.name",
		"com.docker.swarm.task.name":    "swarm.task.name",
		"com.docker.compose.project":    "compose.project",
		"com.docker.compose.service":    "compose.service",
	} {
		if v := strings.TrimSpace(labels[label]); v != "" {
			out[attr] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

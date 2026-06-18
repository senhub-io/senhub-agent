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
	relRunsOn            = "runs_on"
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

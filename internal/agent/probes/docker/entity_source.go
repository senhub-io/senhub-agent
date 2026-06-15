package docker

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeContainer  = "container"
	idKeyContainerID     = "container.id"
	attrContainerName    = "container.name"
	attrContainerImage   = "container.image.name"
	attrContainerRuntime = "container.runtime"
)

// dockerEntitySource feeds the entity rail. Observe() never blocks: it returns
// the last cached snapshot. The cache is refreshed from the container list
// that Collect() fetches each cycle (no extra API call). ok=false before the
// first successful update so the detector does not treat an empty initial
// cache as "all containers deleted".
type dockerEntitySource struct {
	mu    sync.Mutex
	cache entity.Observation
	ready bool
}

// update replaces the entity cache with the current container list.
// Called from Collect() under the probe's own goroutine; must not block.
func (s *dockerEntitySource) update(containers []containerListItem) {
	obs := entity.Observation{}
	for _, c := range containers {
		name := primaryName(c)
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeContainer,
			ID: map[string]any{
				idKeyContainerID: c.ID,
			},
			Attributes: map[string]any{
				attrContainerName:    name,
				attrContainerImage:   c.Image,
				attrContainerRuntime: "docker",
			},
		})
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

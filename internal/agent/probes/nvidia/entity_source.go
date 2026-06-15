package nvidia

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// nvidiaEntitySource emits one service.instance entity per detected GPU.
// The entity's stable identity is the GPU UUID, surfaced as
// service.instance.id="nvidia-gpu://<uuid>".
type nvidiaEntitySource struct {
	mu    sync.Mutex
	cache entity.Observation
	ready bool
}

func newEntitySource() *nvidiaEntitySource {
	return &nvidiaEntitySource{}
}

// update replaces the cached observation with one entity per GPU in the list.
// A nil or empty list results in an empty (but trustworthy) observation —
// signalling that no GPUs are currently visible.
func (s *nvidiaEntitySource) update(gpus []nvidiaGPU) {
	obs := buildEntityObservation(gpus)
	s.mu.Lock()
	s.cache = obs
	s.ready = true
	s.mu.Unlock()
}

// Observe returns the last cached GPU entity set. Returns ok=false before the
// first successful nvidia-smi run so the detector does not treat an initial
// empty observation as "all GPUs deleted".
func (s *nvidiaEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache, s.ready
}

// buildEntityObservation maps a slice of detected GPUs to service.instance
// entities. Each GPU's UUID is its stable, driver-assigned identifier.
func buildEntityObservation(gpus []nvidiaGPU) entity.Observation {
	if len(gpus) == 0 {
		return entity.Observation{}
	}
	obs := entity.Observation{}
	for _, gpu := range gpus {
		if gpu.uuid == "" {
			continue
		}
		instanceID := "nvidia-gpu://" + gpu.uuid
		id := map[string]any{idKeyServiceInstanceID: instanceID}
		attrs := map[string]any{}
		if gpu.name != "" {
			attrs["gpu.name"] = gpu.name
		}
		if gpu.index != "" {
			attrs["gpu.index"] = gpu.index
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       entityTypeServiceInstance,
			ID:         id,
			Attributes: attrs,
		})
	}
	return obs
}

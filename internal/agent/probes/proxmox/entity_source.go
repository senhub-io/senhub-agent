package proxmox

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// proxmoxEntitySource feeds the entity rail with service.instance entities
// for each Proxmox node observed in the last metrics cycle.
//
// Entity schema (frozen with Toise contract):
//
//	type: "service.instance"
//	id:   {"service.instance.id": "proxmox://<endpoint>/<node>"}
//
// The entity carries descriptive attributes (node name, status) that are
// safe to repeat across agents without last-writer-wins flap risk.
type proxmoxEntitySource struct {
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger

	mu      sync.Mutex
	current entity.Observation
	ready   bool
}

func newProxmoxEntitySource(cfg probeConfig, log *logger.ModuleLogger) *proxmoxEntitySource {
	return &proxmoxEntitySource{cfg: cfg, moduleLogger: log}
}

// Observe returns the last entity snapshot built from the metrics cycle.
// Non-blocking and safe to call from the detector goroutine. Returns
// ok=false until the first successful metrics cycle.
func (s *proxmoxEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.ready
}

// refresh rebuilds the entity snapshot from the nodes discovered in the
// last Collect cycle. Called from Collect (probe goroutine) before it
// returns so entities stay in sync with metrics.
func (s *proxmoxEntitySource) refresh(nodes []pveNode) {
	if len(nodes) == 0 {
		return
	}

	obs := entity.Observation{}
	for _, n := range nodes {
		if s.cfg.Node != "" && n.Node != s.cfg.Node {
			continue
		}
		id := map[string]any{
			"service.instance.id": "proxmox://" + s.cfg.Endpoint + "/" + n.Node,
		}
		attrs := map[string]any{
			"proxmox.node":   n.Node,
			"proxmox.status": n.Status,
			"proxmox.endpoint": s.cfg.Endpoint,
		}
		obs.Entities = append(obs.Entities, entity.Entity{
			Type:       "service.instance",
			ID:         id,
			Attributes: attrs,
		})
	}

	s.mu.Lock()
	s.current = obs
	s.ready = true
	s.mu.Unlock()
}

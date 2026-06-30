package proxmox

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// proxmoxEntitySource feeds the entity rail with ONE service.instance entity
// for the Proxmox VE management surface this probe monitors.
//
// Entity schema (ADR 0018 / Toise Q2 contract):
//
//	type: "service.instance"
//	id:   {"service.instance.id": <stable id>}
//
// Stable ID precedence:
//  1. config "instance_name" — verbatim, operator-assigned;
//  2. PVE cluster name — "proxmox:<cluster-name>" (clustered installs);
//  3. agent machine-id fallback — "proxmox@<agent host.id>" (standalone).
//
// Per-node host entities and per-VM compute.vm entities are intentionally NOT
// emitted from this remote probe: they require the node's machine-id, which is
// only available to an on-node agent. (Toise Q2 — add an agent on each PVE
// node to get host + compute.vm entities; this surface covers only the PVE
// management plane.) See #470 / #433.
//
// Outgoing relation: monitors (agent service.instance → proxmox service.instance).
// The agent's identity is read from agentstate; if it is not yet set (entity
// emission not started) the relation is omitted this cycle without error.
type proxmoxEntitySource struct {
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	hostID       string // agent's own machine-id, resolved once at construction

	mu      sync.Mutex
	current entity.Observation
	ready   bool
}

func newProxmoxEntitySource(cfg probeConfig, log *logger.ModuleLogger) *proxmoxEntitySource {
	var hostID string
	if hi, err := common.GetHostIdentity(); err == nil {
		hostID = hi.ID
	}
	return &proxmoxEntitySource{cfg: cfg, moduleLogger: log, hostID: hostID}
}

// Observe returns the last entity snapshot. Non-blocking; returns ok=false
// until the first successful metrics cycle sets the stable ID.
func (s *proxmoxEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.ready
}

// refresh rebuilds the entity snapshot using the resolved stable ID for the
// PVE management surface. clusterName is the PVE cluster name (empty when
// standalone or when the cluster/status call failed). Called from Collect
// (probe goroutine) after a successful node list.
func (s *proxmoxEntitySource) refresh(clusterName string) {
	instanceID := s.resolveInstanceID(clusterName)

	proxmoxID := map[string]any{"service.instance.id": instanceID}

	attrs := map[string]any{
		"service.name":   "proxmox",
		"server.address": s.cfg.Endpoint,
	}

	pve := entity.Entity{
		Type:       "service.instance",
		ID:         proxmoxID,
		Attributes: attrs,
	}

	obs := entity.Observation{
		Entities: []entity.Entity{pve},
	}

	// Emit a monitors edge from the agent's own service.instance to the
	// proxmox surface, so Toise can associate this probe's telemetry with
	// the management plane it covers. Skip the edge when the agent identity
	// is not yet available (entity emission not started).
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     proxmoxID,
		})
	}

	// runs_on edge: proxmox → host when the PVE endpoint is local (loopback), so
	// an on-host management surface hangs off the host it runs on instead of
	// floating with only its monitors anchor. A remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn("service.instance", proxmoxID, hostFromEndpoint(s.cfg.Endpoint), s.hostID); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	s.mu.Lock()
	s.current = obs
	s.ready = true
	s.mu.Unlock()
}

// hostFromEndpoint extracts the host (no port) from the PVE endpoint URL so the
// runs_on gate can tell a loopback-monitored surface from a remote one. Returns
// the raw value unchanged when it is not a parseable URL.
func hostFromEndpoint(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return endpoint
}

// resolveInstanceID applies the stable-ID precedence rules:
//  1. operator-supplied instance_name (verbatim);
//  2. PVE cluster name → "proxmox:<cluster-name>";
//  3. agent machine-id fallback → "proxmox@<host.id>".
func (s *proxmoxEntitySource) resolveInstanceID(clusterName string) string {
	if s.cfg.InstanceName != "" {
		return s.cfg.InstanceName
	}
	if clusterName != "" {
		return "proxmox:" + clusterName
	}
	// Fallback: use the agent's own host.id (machine-id) so the proxmox
	// surface is tied to the machine running the probe (standalone installs,
	// or when the cluster/status call is unavailable) — the precedence-2
	// <service.name>@<host.id> convention. Never use the endpoint — endpoint
	// is descriptive (network-routable address), not identity.
	if s.hostID != "" {
		return "proxmox@" + s.hostID
	}
	// Last resort: a static sentinel that signals "not yet resolved".
	// The ready flag stays false until a valid cycle sets the cluster
	// name or the agent ID becomes available, so this branch should be
	// transient at most.
	return "proxmox@unknown"
}

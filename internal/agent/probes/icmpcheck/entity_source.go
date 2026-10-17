package icmpcheck

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

// icmpEntitySource feeds the entity rail with the network.address
// entities this probe monitors. For ICMP there is no service.listener
// distinction: a local loopback target is intentionally ignored
// (rule 2 local ICMP), while a remote target becomes a network.address.
type icmpEntitySource struct {
	hostID string
	mu     sync.RWMutex
	obs    entity.Observation
	ready  bool
}

func newICMPEntitySource(hostID string) *icmpEntitySource {
	return &icmpEntitySource{hostID: hostID}
}

func (s *icmpEntitySource) setObservation(obs entity.Observation) {
	s.mu.Lock()
	s.obs = obs
	s.ready = true
	s.mu.Unlock()
}

// Observe implements entity.Source.
func (s *icmpEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return entity.Observation{}, false
	}
	return s.obs, true
}

func isLoopbackTarget(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLoopback()
}

func canonicalICMPAddr(res pingResult) (netip.Addr, bool) {
	if ip, err := netip.ParseAddr(res.target); err == nil {
		return ip.Unmap(), true
	}
	if res.resolvedIP != "" {
		if ip, err := netip.ParseAddr(res.resolvedIP); err == nil {
			return ip.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func (p *ICMPCheckProbe) entityForResult(res pingResult) (entity.Entity, bool) {
	if isLoopbackTarget(res.target) {
		return entity.Entity{}, false
	}
	addr, ok := canonicalICMPAddr(res)
	if !ok {
		return entity.Entity{}, false
	}
	if !entity.AddressEdgeAllowed(addr.String()) {
		return entity.Entity{}, false
	}

	attrs := map[string]any{}
	if p.entitySrc.hostID != "" {
		attrs["senhub.check.probe_host.id"] = p.entitySrc.hostID
	}
	return entity.Entity{
		Type:       "network.address",
		ID:         map[string]any{"network.address": addr.String()},
		Attributes: attrs,
	}, true
}

func (p *ICMPCheckProbe) identityTagsForResult(res pingResult) []tags.Tag {
	ent, ok := p.entityForResult(res)
	if !ok {
		return nil
	}
	return idTags(ent.ID)
}

func idTags(id map[string]any) []tags.Tag {
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]tags.Tag, 0, len(keys))
	for _, k := range keys {
		out = append(out, tags.Tag{Key: k, Value: fmt.Sprint(id[k])})
	}
	return out
}

// buildEntityObservation rebuilds the entity snapshot from one Collect's
// results. agentID is the agent service.instance.id; an empty agentID skips
// the monitors relation.
func (p *ICMPCheckProbe) buildEntityObservation(results []pingResult, agentID string) entity.Observation {
	obs := entity.Observation{}
	seenEntities := map[string]bool{}

	for _, res := range results {
		ent, ok := p.entityForResult(res)
		if !ok {
			continue
		}
		key := entityKey(ent)
		if seenEntities[key] {
			continue
		}
		seenEntities[key] = true
		obs.Entities = append(obs.Entities, ent)
	}

	if agentID == "" {
		return obs
	}

	fromID := map[string]any{"service.instance.id": agentID}
	seenRelations := map[string]bool{}
	for _, ent := range obs.Entities {
		rel := entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   fromID,
			ToType:   ent.Type,
			ToID:     ent.ID,
		}
		key := relationKey(rel)
		if seenRelations[key] {
			continue
		}
		seenRelations[key] = true
		obs.Relations = append(obs.Relations, rel)
	}
	return obs
}

func entityKey(e entity.Entity) string {
	return e.Type + "|" + strMapKey(e.ID)
}

func relationKey(r entity.Relation) string {
	return r.Type + "|" + r.FromType + "|" + strMapKey(r.FromID) + "|" + r.ToType + "|" + strMapKey(r.ToID)
}

func strMapKey(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", m[k])
		b.WriteByte('|')
	}
	return b.String()
}

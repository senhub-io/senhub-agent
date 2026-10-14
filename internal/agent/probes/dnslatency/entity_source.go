package dnslatency

import (
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/toise-dev/toise/pkg/emit"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

// dnsResolverTransport is the transport used to reach a DNS resolver.
const dnsResolverTransport = "udp"

type dnsEntitySource struct {
	hostID string

	mu      sync.RWMutex
	built   bool
	targets []dnsEntityTarget
}

type dnsEntityTarget struct {
	resolver     string
	entity       *entity.Entity
	relation     *entity.Relation
	identityTags []tags.Tag
}

func newDNSEntitySource(hostID string) *dnsEntitySource {
	return &dnsEntitySource{hostID: hostID}
}

// rebuild replaces the immutable target snapshot. Resolvers do not change at
// runtime; calling this on every Collect keeps the entity rail in lock-step
// with the probe's effective target list.
func (s *dnsEntitySource) rebuild(resolvers []string, agentID string) {
	targets := make([]dnsEntityTarget, 0, len(resolvers))
	for _, resolver := range resolvers {
		targets = append(targets, s.buildTarget(resolver, agentID))
	}

	s.mu.Lock()
	s.targets = targets
	s.built = true
	s.mu.Unlock()
}

func (s *dnsEntitySource) buildTarget(resolver, agentID string) dnsEntityTarget {
	t := dnsEntityTarget{resolver: resolver}

	if resolver == systemResolverLabel {
		return t
	}

	host, port, ok := splitResolverHostPort(resolver)
	if !ok {
		return t
	}

	if isLocalTarget(host) {
		if s.hostID == "" {
			return t
		}
		listenerID := entity.ListenerEndpoint(s.hostID, port, dnsResolverTransport)
		id := map[string]any{"service.endpoint": listenerID}
		t.identityTags = tagsFromID(id)
		if agentID != "" {
			t.relation = &entity.Relation{
				Type:     "monitors",
				FromType: "service.instance",
				FromID:   map[string]any{"service.instance.id": agentID},
				ToType:   "service.listener",
				ToID:     id,
			}
		}
		return t
	}

	addr := normalizeResolverAddress(host)
	id := remoteEndpointID(addr, port, s.hostID)
	t.entity = &entity.Entity{Type: "network.endpoint", ID: id}
	t.identityTags = tagsFromID(id)
	if agentID != "" {
		t.relation = &entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "network.endpoint",
			ToID:     id,
		}
	}
	return t
}

func remoteEndpointID(addr string, port int, hostID string) map[string]any {
	rel := emit.DependsOnLocalEndpoint(addr, port, dnsResolverTransport, hostID)
	id := make(map[string]any, len(rel.TargetID))
	for k, v := range rel.TargetID {
		id[k] = v
	}
	return id
}

func tagsFromID(id map[string]any) []tags.Tag {
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]tags.Tag, 0, len(id))
	for _, k := range keys {
		if v, ok := id[k].(string); ok {
			out = append(out, tags.Tag{Key: k, Value: v})
		}
	}
	return out
}

func splitResolverHostPort(resolver string) (string, int, bool) {
	host, portStr, err := net.SplitHostPort(resolver)
	if err != nil {
		// parseConfig normally normalizes bare hosts to host:53, but tests may
		// construct an entity source with a bare host.
		host = resolver
		portStr = "53"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func isLocalTarget(host string) bool {
	if strings.ToLower(host) == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}

func normalizeResolverAddress(host string) string {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String()
	}
	return strings.ToLower(host)
}

func (s *dnsEntitySource) identityTags(resolver string) []tags.Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.targets {
		if t.resolver == resolver {
			out := make([]tags.Tag, len(t.identityTags))
			copy(out, t.identityTags)
			return out
		}
	}
	return nil
}

// Observe implements entity.Source. Before the first Collect there is no
// snapshot yet, so it reports ok=false. After that the snapshot is immutable
// until the next Collect replaces it.
func (s *dnsEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	if !s.built {
		s.mu.RUnlock()
		return entity.Observation{}, false
	}
	targets := make([]dnsEntityTarget, len(s.targets))
	copy(targets, s.targets)
	s.mu.RUnlock()

	var obs entity.Observation
	for _, t := range targets {
		if t.entity != nil {
			obs.Entities = append(obs.Entities, *t.entity)
		}
		if t.relation != nil {
			obs.Relations = append(obs.Relations, *t.relation)
		}
	}
	return obs, true
}

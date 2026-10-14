package ntp

import (
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"github.com/toise-dev/toise/pkg/emit"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

// ntpEntitySource feeds the entity rail with the NTP servers this probe checks.
//
// NTP is an active check: the targets are configured servers, not things the
// agent discovers. The source therefore rebuilds a snapshot from the configured
// server list once per Collect. It emits a network.endpoint entity for each
// remote server and, for a loopback target, only a monitors relation toward the
// service.listener identity already owned by the host-service inventory.
type ntpEntitySource struct {
	hostID string
	// agentID is an override point for tests; production uses
	// agentstate.GetAgentInstanceID.
	agentID func() string

	mu        sync.RWMutex
	targets   []ntpTarget
	byServer  map[string]ntpTarget
	collected bool
}

type ntpTarget struct {
	server     string
	targetType string
	targetID   map[string]string
}

func newNTPEntitySource(hostID string) *ntpEntitySource {
	return &ntpEntitySource{
		hostID:  hostID,
		agentID: agentstate.GetAgentInstanceID,
	}
}

func (s *ntpEntitySource) rebuild(servers []string) {
	byServer := make(map[string]ntpTarget, len(servers))
	targets := make([]ntpTarget, 0, len(servers))
	for _, server := range servers {
		t := buildNTPTarget(server, s.hostID)
		byServer[server] = t
		targets = append(targets, t)
	}

	s.mu.Lock()
	s.targets = targets
	s.byServer = byServer
	s.collected = true
	s.mu.Unlock()
}

// buildNTPTarget parses one configured NTP server into the entity identity the
// target represents.
//
// The transport is always "udp"; the port is explicit from a host:port server
// or 123 otherwise, mirroring client.query.
func buildNTPTarget(server, hostID string) ntpTarget {
	const defaultPortNumber = 123

	host, port := splitNTPServer(server, defaultPortNumber)
	ip, isIP := parseIP(host)
	local := strings.EqualFold(host, "localhost") || (isIP && ip.IsLoopback())

	if local {
		t := ntpTarget{server: server, targetType: "service.listener"}
		if hostID != "" {
			t.targetID = map[string]string{
				"service.endpoint": entity.ListenerEndpoint(hostID, port, "udp"),
			}
		}
		return t
	}

	// The endpoint key must use the configured DNS name lowercased, or the
	// canonical IP literal -- never the resolved IP, which changes. The SDK
	// helper canonicalises IP literals and adds host.id only for host-scoped
	// link-local/loopback addresses; loopback was already handled above.
	addr := host
	if isIP {
		addr = ip.String()
	} else {
		addr = strings.ToLower(host)
	}
	rel := emit.DependsOnLocalEndpoint(addr, port, "udp", hostID)

	return ntpTarget{
		server:     server,
		targetType: "network.endpoint",
		targetID:   rel.TargetID,
	}
}

// splitNTPServer returns the host and port for a configured server string. It
// mirrors query: an absent port means 123.
func splitNTPServer(server string, defaultPort int) (string, int) {
	if h, p, err := net.SplitHostPort(server); err == nil && h != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return h, n
		}
		return h, defaultPort
	}
	return server, defaultPort
}

func parseIP(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// identityTags returns the entity identity keys for a configured server, for
// use as datapoint tags. A target with no entity (local target with empty
// hostID) returns nil.
func (s *ntpEntitySource) identityTags(server string) []tags.Tag {
	s.mu.RLock()
	t, ok := s.byServer[server]
	s.mu.RUnlock()
	if !ok || len(t.targetID) == 0 {
		return nil
	}

	out := make([]tags.Tag, 0, len(t.targetID))
	for k, v := range t.targetID {
		out = append(out, tags.Tag{Key: k, Value: v})
	}
	return out
}

// Observe implements entity.Source. It returns the target entities plus
// monitors relations after the first Collect has rebuilt the snapshot.
func (s *ntpEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	targets := s.targets
	collected := s.collected
	s.mu.RUnlock()

	if !collected {
		return entity.Observation{}, false
	}

	var obs entity.Observation
	agentID := s.currentAgentID()

	for _, t := range targets {
		if len(t.targetID) == 0 {
			continue
		}

		toID := make(map[string]any, len(t.targetID))
		for k, v := range t.targetID {
			toID[k] = v
		}

		if t.targetType == "network.endpoint" {
			attrs := make(map[string]any, len(t.targetID))
			for k, v := range t.targetID {
				attrs[k] = v
			}
			obs.Entities = append(obs.Entities, entity.Entity{
				Type:       "network.endpoint",
				ID:         toID,
				Attributes: attrs,
			})
		}

		if agentID == "" {
			continue
		}
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   t.targetType,
			ToID:     toID,
		})
	}

	return obs, true
}

func (s *ntpEntitySource) currentAgentID() string {
	if s.agentID == nil {
		return ""
	}
	return s.agentID()
}

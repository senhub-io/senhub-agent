package tcpdial

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

// tcpdialEntitySource feeds the entity rail with the network endpoints this
// probe monitors. Local loopback targets point at a service.endpoint owned by
// another producer, so only the monitors relation is emitted; remote targets
// emit their own network.endpoint entity.
type tcpdialEntitySource struct {
	mu         sync.RWMutex
	ready      bool
	obs        entity.Observation
	getAgentID func() string
}

func newTCPDialEntitySource(getAgentID func() string) *tcpdialEntitySource {
	return &tcpdialEntitySource{getAgentID: getAgentID}
}

type targetInfo struct {
	targetType   string
	targetID     map[string]any
	emitsEntity  bool
	identityTags []tags.Tag
}

func (s *tcpdialEntitySource) rebuild(targets []string, hostID string) {
	agentID := s.getAgentID()
	var obs entity.Observation

	for _, configured := range targets {
		info := buildTargetInfo(configured, hostID)
		if info.targetID == nil {
			continue
		}

		if info.emitsEntity {
			obs.Entities = append(obs.Entities, entity.Entity{
				Type: info.targetType,
				ID:   info.targetID,
			})
		}

		if agentID != "" {
			obs.Relations = append(obs.Relations, entity.Relation{
				Type:     "monitors",
				FromType: "service.instance",
				FromID:   map[string]any{"service.instance.id": agentID},
				ToType:   info.targetType,
				ToID:     info.targetID,
			})
		}
	}

	s.mu.Lock()
	s.obs = obs
	s.ready = true
	s.mu.Unlock()
}

func (s *tcpdialEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return entity.Observation{}, false
	}
	return s.obs, true
}

func buildTargetInfo(configured, hostID string) targetInfo {
	host, portStr, err := net.SplitHostPort(configured)
	if err != nil {
		return targetInfo{}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return targetInfo{}
	}

	addr := strings.ToLower(host)
	if isLocalHost(addr) {
		if hostID == "" {
			return targetInfo{}
		}
		id := entity.ListenerEndpoint(hostID, port, "tcp")
		m := map[string]any{"service.endpoint": id}
		return targetInfo{
			targetType:   "service.listener",
			targetID:     m,
			identityTags: tagsFromEntityID(m),
		}
	}

	rel := emit.DependsOnLocalEndpoint(addr, port, "tcp", hostID)
	m := toAnyMap(rel.TargetID)
	return targetInfo{
		targetType:   "network.endpoint",
		targetID:     m,
		emitsEntity:  true,
		identityTags: tagsFromEntityID(m),
	}
}

func isLocalHost(addr string) bool {
	if addr == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLoopback()
}

func toAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func tagsFromEntityID(m map[string]any) []tags.Tag {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]tags.Tag, 0, len(keys))
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			out = append(out, tags.Tag{Key: k, Value: v})
		}
	}
	return out
}

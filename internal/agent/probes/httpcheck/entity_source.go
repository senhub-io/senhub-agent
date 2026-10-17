package httpcheck

import (
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/toise-dev/toise/pkg/emit"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

// httpTargetKind discriminates how a configured target contributes to the
// entity rail. A target either points at a remote network endpoint, at a
// local service.listener owned by another producer, or at nothing we can
// identify from the configured URL alone.
type httpTargetKind int

const (
	httpTargetNone httpTargetKind = iota
	httpTargetRemoteEndpoint
	httpTargetLocalListener
)

// httpTargetInfo carries the entity identity extracted from one target. For
// remote non-ICMP targets the entity itself is emitted; for local non-ICMP
// targets only the relation is emitted because the listener entity is owned
// by the host service inventory producer.
type httpTargetInfo struct {
	kind     httpTargetKind
	id       map[string]any
	entity   *entity.Entity
	relation *entity.Relation
}

func (i httpTargetInfo) identityTags() []tags.Tag {
	if len(i.id) == 0 {
		return nil
	}

	keys := make([]string, 0, len(i.id))
	for k := range i.id {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ts := make([]tags.Tag, 0, len(keys))
	for _, k := range keys {
		ts = append(ts, tags.Tag{Key: k, Value: fmt.Sprint(i.id[k])})
	}
	return ts
}

// httpEntitySource feeds the entity rail with the list of hosts/endpoints
// this probe checks. The target list is static, so the source rebuilds its
// observation on every Collect and returns that snapshot to Observe until the
// next Collect replaces it.
type httpEntitySource struct {
	mu        sync.RWMutex
	hostID    string
	agentID   func() string
	snapshot  *entity.Observation
	collected bool
}

func newHTTPCheckEntitySource(hostID string) *httpEntitySource {
	return &httpEntitySource{
		hostID:  hostID,
		agentID: agentstate.GetAgentInstanceID,
	}
}

func (s *httpEntitySource) rebuild(targets []string) map[string]httpTargetInfo {
	agentID := ""
	if s.agentID != nil {
		agentID = s.agentID()
	}

	obs, byTarget := buildObservation(targets, s.hostID, agentID)

	s.mu.Lock()
	s.snapshot = &obs
	s.collected = true
	s.mu.Unlock()

	return byTarget
}

// Observe implements entity.Source. Before the first Collect there is no
// target observation yet, so it reports false; afterwards it reports the last
// rebuilt snapshot.
func (s *httpEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.collected || s.snapshot == nil {
		return entity.Observation{}, false
	}
	return *s.snapshot, true
}

func buildObservation(targets []string, hostID, agentID string) (entity.Observation, map[string]httpTargetInfo) {
	var obs entity.Observation
	byTarget := make(map[string]httpTargetInfo, len(targets))

	for _, target := range targets {
		info := parseHTTPTarget(target, hostID, agentID)
		byTarget[target] = info

		if info.entity != nil {
			obs.Entities = append(obs.Entities, *info.entity)
		}
		if info.relation != nil {
			obs.Relations = append(obs.Relations, *info.relation)
		}
	}

	return obs, byTarget
}

func parseHTTPTarget(target, hostID, agentID string) httpTargetInfo {
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" {
		return httpTargetInfo{kind: httpTargetNone}
	}

	host := strings.ToLower(u.Hostname())
	port, err := httpPort(u)
	if err != nil {
		return httpTargetInfo{kind: httpTargetNone}
	}

	const transport = "tcp"

	if isLocalHost(host) {
		if hostID == "" {
			return httpTargetInfo{kind: httpTargetNone}
		}

		id := map[string]any{
			"service.endpoint": entity.ListenerEndpoint(hostID, port, transport),
		}
		info := httpTargetInfo{kind: httpTargetLocalListener, id: id}

		if agentID != "" {
			info.relation = &entity.Relation{
				Type:     "monitors",
				FromType: "service.instance",
				FromID:   map[string]any{"service.instance.id": agentID},
				ToType:   "service.listener",
				ToID:     id,
			}
		}
		return info
	}

	rel := emit.DependsOnLocalEndpoint(host, port, transport, hostID)
	id := make(map[string]any, len(rel.TargetID))
	for k, v := range rel.TargetID {
		id[k] = v
	}

	info := httpTargetInfo{kind: httpTargetRemoteEndpoint, id: id}
	info.entity = &entity.Entity{Type: "network.endpoint", ID: id}

	if agentID != "" {
		info.relation = &entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "network.endpoint",
			ToID:     id,
		}
	}
	return info
}

func httpPort(u *url.URL) (int, error) {
	if p := u.Port(); p != "" {
		return strconv.Atoi(p)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return 443, nil
	default:
		return 80, nil
	}
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.Unmap().IsLoopback()
}

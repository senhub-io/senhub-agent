package ceph

import (
	"encoding/json"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// fetchFsidFunc is the tech-id resolver injected at construction. The
// production implementation calls GET /api/cluster; tests inject a stub.
type fetchFsidFunc func() (string, error)

// hostIDFunc resolves the local host identity for the precedence-2 fallback.
// Injected at construction so tests never touch the OS.
type hostIDFunc func() string

// cephEntitySource reports the monitored Ceph cluster as a service.instance
// entity.
//
// ID resolution order (D1 / option A, precedence-1 service.instance):
//  1. operator config key "instance_name" → pinned at construction.
//  2. tech-reported cluster fsid fetched on the first successful Collect
//     → "ceph:<fsid>" (pinned once, never changed).
//  3. precedence-2 fallback "ceph@<host.id>" when the tech id is
//     permanently unreachable; "ceph" as last resort.
//
// The entity and the monitors edge are NOT emitted before the id is pinned
// (Observe returns ok=false), so the consumer never sees a transient
// network-derived id that would be re-keyed on the next successful fetch.
type cephEntitySource struct {
	// instanceName is the operator-configured override (empty = not set).
	instanceName string
	// fetchFsid resolves the cluster fsid from the REST API.
	fetchFsid fetchFsidFunc
	// getHostID resolves the local machine-id for the precedence-2 fallback.
	getHostID hostIDFunc

	// endpoint carries server.address / server.port as descriptive attributes.
	endpoint string

	mu     sync.Mutex
	pinned bool
	id     string
	obs    entity.Observation
}

func newCephEntitySource(
	instanceName string,
	endpoint string,
	fetchFsid fetchFsidFunc,
	getHostID hostIDFunc,
) *cephEntitySource {
	s := &cephEntitySource{
		instanceName: instanceName,
		endpoint:     endpoint,
		fetchFsid:    fetchFsid,
		getHostID:    getHostID,
	}
	// Precedence 1: instance_name is known at construction time.
	if instanceName != "" {
		s.pin(instanceName)
	}
	return s
}

// Observe returns the Ceph service.instance entity and the monitors edge.
// Returns ok=false (and an empty observation) until the id has been pinned —
// the caller must not emit the entity before the id is stable.
func (s *cephEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pinned {
		return entity.Observation{}, false
	}
	return s.obs, true
}

// pinID attempts to resolve the cluster fsid from the REST API and pin the
// tech id. If the API call fails and a host-based fallback is available, the
// fallback is pinned instead. Must be called from Collect (which already holds
// a valid auth token).
//
// Callers check pinned before calling, so this is a one-shot operation.
func (s *cephEntitySource) pinID() {
	s.mu.Lock()
	alreadyPinned := s.pinned
	s.mu.Unlock()
	if alreadyPinned {
		return
	}

	// Precedence 2: tech-reported fsid.
	if fsid, err := s.fetchFsid(); err == nil && fsid != "" {
		s.mu.Lock()
		if !s.pinned {
			s.pin("ceph:" + fsid)
		}
		s.mu.Unlock()
		return
	}

	// Precedence 3: host-based fallback.
	hid := s.getHostID()
	if hid != "" {
		s.mu.Lock()
		if !s.pinned {
			s.pin("ceph@" + hid)
		}
		s.mu.Unlock()
		return
	}

	// Last resort.
	s.mu.Lock()
	if !s.pinned {
		s.pin("ceph")
	}
	s.mu.Unlock()
}

// pin stores id and builds the cached observation. Must be called with mu held
// or before the source is shared (constructor).
func (s *cephEntitySource) pin(id string) {
	s.id = id
	s.pinned = true

	attrs := map[string]any{
		"service.name": "ceph",
	}
	var serverAddr string
	if s.endpoint != "" {
		host, port := splitHostPort(s.endpoint)
		serverAddr = host
		if host != "" {
			attrs["server.address"] = host
		}
		if port != "" {
			attrs["server.port"] = port
		}
	}

	svcID := map[string]any{idKeyServiceInstanceID: id}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         svcID,
				Attributes: attrs,
			},
		},
	}

	// monitors edge: agent → ceph cluster. Emitted only when the agent id is
	// available (SetAgentInstanceID has been called by the entity foundation).
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: entityTypeServiceInstance,
			FromID:   map[string]any{idKeyServiceInstanceID: agentID},
			ToType:   entityTypeServiceInstance,
			ToID:     svcID,
		})
	}

	// runs_on edge: cluster → host when the endpoint is local (loopback), so a
	// locally-monitored Ceph hangs off the host it runs on instead of floating.
	// The id is tech ("ceph:<fsid>") or host-scoped ("ceph@<hid>") — it never
	// embeds the address — so loopback passes the collapse guard; a remote
	// endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn(entityTypeServiceInstance, svcID, serverAddr, s.getHostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	s.obs = obs
}

// splitHostPort extracts host and port strings from an endpoint URL or
// host:port string. Best-effort: returns empty strings on failure.
func splitHostPort(endpoint string) (host, port string) {
	// Strip scheme.
	s := endpoint
	if i := indexOf(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Strip path.
	if i := indexOf(s, "/"); i >= 0 {
		s = s[:i]
	}
	// Split host:port.
	if i := lastIndexOf(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// defaultFetchFsid returns a fetchFsidFunc that calls GET /api/cluster on the
// given CephProbe to retrieve the cluster fsid. The probe's http client and
// token are used directly, so this must only be called after authentication.
func defaultFetchFsid(p *CephProbe) fetchFsidFunc {
	return func() (string, error) {
		raw, err := p.apiGet("/api/cluster")
		if err != nil {
			return "", err
		}
		var result struct {
			FSID string `json:"fsid"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return "", err
		}
		return result.FSID, nil
	}
}

// defaultGetHostID returns a hostIDFunc that resolves via common.GetHostIdentity.
func defaultGetHostID() hostIDFunc {
	return func() string {
		hi, err := common.GetHostIdentity()
		if err != nil {
			return ""
		}
		return hi.ID
	}
}

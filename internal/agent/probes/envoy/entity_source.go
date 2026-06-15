package envoy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// envoyEntitySource feeds the entity rail with the single Envoy instance this
// probe monitors. Toise D1 contract (option A — tech-reported persistent id):
//
//   - Type: "service.instance"
//   - ID:   {"service.instance.id": <pinned id>}   (see id-resolution rules below)
//   - Attrs: service.name, server.address, server.port (int64)
//
// Id resolution, by precedence:
//  1. operator config key "instance_name" — pinned at construction time, never changes.
//  2. Envoy node id fetched from GET /server_info (node.id from bootstrap) →
//     formatted as "envoy:<node.id>"; pinned on the first successful collect cycle.
//  3. Host machine id fallback "envoy@<host.id>" — pinned once the tech fetch
//     has definitely failed (e.g. endpoint unreachable to learn it).
//
// Immutability invariant: the pinned id is written exactly once (sync.Once) and
// never changes afterwards. The entity is NOT emitted before the id is pinned so
// Toise never receives a preliminary id that a later tech-id fetch would re-key.
//
// Monitors edge: once the entity is emitted, a "monitors" relation from the
// agent's service.instance to this instance is included in the observation.
// The edge is skipped when the agent id is not yet available (agentstate returns "").
type envoyEntitySource struct {
	// descAttr holds the entity's descriptive attributes (service.name, server.address,
	// server.port). Set once at construction, never modified.
	descAttr map[string]any

	// fetchNodeID fetches the Envoy /server_info endpoint and returns the
	// node.id field. Returns "" when the field is empty or the call fails.
	// Injected at construction; overridable in tests.
	fetchNodeID func() string

	// hostID returns the host machine id for the precedence-3 fallback.
	// Injected at construction; overridable in tests.
	hostID func() string

	// idOnce ensures the pinned id is written exactly once.
	idOnce   sync.Once
	pinnedID string
	idPinned bool // true once idOnce has fired and pinnedID is set

	// up is the reachability state updated by the probe after each scrape.
	mu sync.RWMutex
	up bool
}

// serverInfoResponse is the minimal subset of the Envoy /server_info JSON
// response we need for id resolution. The full schema has many more fields;
// we decode only what we need.
type serverInfoResponse struct {
	Node struct {
		ID string `json:"id"`
	} `json:"node"`
}

// newEnvoyEntitySource builds an entity source for an Envoy probe.
//   - instanceName: operator override ("instance_name" config key); if non-empty
//     the id is pinned immediately without a tech fetch.
//   - endpoint: the Envoy admin endpoint URL (e.g. "http://localhost:9901").
//   - addr, portStr: parsed host and port from the endpoint, used as descriptive attrs.
//   - client: the probe's shared HTTP client.
//   - hostIDFunc: returns the host machine id; nil falls back to the production impl.
func newEnvoyEntitySource(
	instanceName string,
	endpoint string,
	addr string,
	portInt int64,
	client *http.Client,
	hostIDFunc func() string,
) *envoyEntitySource {
	descAttr := map[string]any{
		"service.name":   "envoy",
		"server.address": addr,
		"server.port":    portInt,
	}

	if hostIDFunc == nil {
		hostIDFunc = productionHostID
	}

	s := &envoyEntitySource{
		descAttr: descAttr,
		hostID:   hostIDFunc,
		fetchNodeID: func() string {
			return fetchEnvoyNodeID(client, endpoint)
		},
	}

	// Precedence 1: operator override — pin at construction.
	if instanceName != "" {
		s.idOnce.Do(func() {
			s.pinnedID = instanceName
			s.idPinned = true
		})
	}

	return s
}

// productionHostID resolves the host machine id via the common package at runtime.
func productionHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil || hi.ID == "" {
		return ""
	}
	return hi.ID
}

// tryPin attempts to pin the service.instance.id by fetching the Envoy node id.
// If the tech fetch succeeds and node id is non-empty, pins "envoy:<node.id>".
// If the tech fetch returns empty (node.id is absent or the call fails but the
// probe is currently up), we accept the degraded state and pin the host-id
// fallback so the entity can start being emitted.
//
// Returns true once the id has been pinned (regardless of path taken).
func (s *envoyEntitySource) tryPin() bool {
	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()

	if !up {
		// Not yet reachable — don't degrade to fallback yet; wait for a
		// successful collect to learn whether the node id is available.
		return false
	}

	s.idOnce.Do(func() {
		nodeID := s.fetchNodeID()
		if nodeID != "" {
			s.pinnedID = fmt.Sprintf("envoy:%s", nodeID)
		} else {
			// Tech id unavailable (node.id empty in bootstrap). Degrade to
			// host-id fallback so the entity can be emitted.
			hid := s.hostID()
			if hid != "" {
				s.pinnedID = fmt.Sprintf("envoy@%s", hid)
			} else {
				s.pinnedID = "envoy"
			}
		}
		s.idPinned = true
	})
	return s.idPinned
}

// setReachable updates the probe's liveness state after each scrape.
// Call with up=true on success, up=false on any failure.
func (s *envoyEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false when:
//   - the id has not yet been pinned (no stable identity available), OR
//   - Envoy was not reachable on the last cycle.
//
// Never emits a placeholder id that a future tech-id fetch would re-key.
func (s *envoyEntitySource) Observe() (entity.Observation, bool) {
	// Attempt to pin the id lazily, on the first successful collect cycle.
	if !s.idPinned {
		if !s.tryPin() {
			return entity.Observation{}, false
		}
	}

	s.mu.RLock()
	up := s.up
	s.mu.RUnlock()
	if !up {
		return entity.Observation{}, false
	}

	targetID := map[string]any{"service.instance.id": s.pinnedID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         targetID,
			Attributes: s.descAttr,
		}},
	}

	// Monitors edge: agent → target. Skip when the agent id is not yet known.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     targetID,
		})
	}

	return obs, true
}

// fetchEnvoyNodeID calls GET /server_info on the Envoy admin endpoint and
// extracts the node.id field from the JSON response. Returns "" on any
// error or when node.id is absent / empty.
func fetchEnvoyNodeID(client *http.Client, endpoint string) string {
	url := endpoint + "/server_info"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}

	var info serverInfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return ""
	}
	return info.Node.ID
}

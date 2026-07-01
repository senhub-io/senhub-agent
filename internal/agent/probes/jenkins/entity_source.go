package jenkins

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeServiceInstance = "service.instance"
	idKeyServiceInstanceID    = "service.instance.id"
)

// instanceIdentityResponse is the JSON shape returned by Jenkins'
// /instance-identity/api/json endpoint (Instance Identity plugin, bundled
// since Jenkins 2.401). The RSA public key is Base64-encoded DER; we
// fingerprint it with SHA-256 to produce a compact, stable identifier.
type instanceIdentityResponse struct {
	PublicKey string `json:"publicKey"`
}

// entitySource emits the Jenkins controller as a single service.instance
// entity and a monitors edge from the agent. The service.instance.id is
// resolved lazily on the first successful observation (Toise D1 option A):
//
//  1. operator config key instance_name — verbatim, pinned at construction.
//  2. tech id fetched from the target: fingerprint of the RSA public key
//     from /instance-identity/api/json, formatted as "jenkins:<hex20>".
//  3. precedence-2 fallback: "jenkins@<host.id>" resolved by hostIDFn.
//
// The entity is NOT emitted before the id is pinned (Observe returns
// ok=false) so we never emit a network-derived placeholder that would
// re-key the entity in the consumer when the real id is learned.
type entitySource struct {
	// fetchIdentity asks the probe's HTTP client for the Jenkins instance id.
	// Returns a non-empty id string on success or ("", err) when unreachable.
	// Called at most once (the result pins pinnedID).
	fetchIdentity func() (string, error)

	// hostIDFn returns the host's machine-id for the precedence-2 fallback.
	// Injected at construction so tests can supply a deterministic value.
	hostIDFn func() string

	// descriptive attributes captured at construction, emitted on every state.
	serverAddress string
	serverPort    string

	mu       sync.Mutex
	pinnedID string // empty = not yet pinned
	degraded bool   // true = gave up on tech id, using fallback
	version  string // service.version from the X-Jenkins header, "" until seen
}

// setVersion records the controller version from the X-Jenkins response header
// so it rides the entity as the descriptive service.version attribute
// (toise#216 AT1). Empty values are ignored.
func (s *entitySource) setVersion(v string) {
	if v == "" {
		return
	}
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// currentVersion returns the last seen controller version (may be "").
func (s *entitySource) currentVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// newEntitySource builds the source. If instanceName is non-empty it is used
// verbatim as the service.instance.id (pinned immediately). Otherwise the id
// is resolved lazily on the first Observe() call.
func newEntitySource(instanceName, serverAddress, serverPort string, fetchIdentity func() (string, error), hostIDFn func() string) *entitySource {
	s := &entitySource{
		fetchIdentity: fetchIdentity,
		hostIDFn:      hostIDFn,
		serverAddress: serverAddress,
		serverPort:    serverPort,
	}
	if instanceName != "" {
		s.pinnedID = instanceName
	}
	return s
}

// Observe returns the controller entity and the monitors edge. Returns
// ok=false until the service.instance.id is pinned so the consumer never
// sees a placeholder id that would be re-keyed on the next cycle.
func (s *entitySource) Observe() (entity.Observation, bool) {
	id := s.resolveID()
	if id == "" {
		return entity.Observation{}, false
	}

	targetEntityID := map[string]any{idKeyServiceInstanceID: id}
	attrs := map[string]any{
		"service.name":   "jenkins",
		"server.address": s.serverAddress,
		"server.port":    s.serverPort,
	}
	if v := s.currentVersion(); v != "" {
		attrs["service.version"] = v
	}
	obs := entity.Observation{
		Entities: []entity.Entity{
			{
				Type:       entityTypeServiceInstance,
				ID:         targetEntityID,
				Attributes: attrs,
			},
		},
	}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: entityTypeServiceInstance,
			FromID:   map[string]any{idKeyServiceInstanceID: agentID},
			ToType:   entityTypeServiceInstance,
			ToID:     targetEntityID,
		})
	}

	// runs_on edge: controller → host when the endpoint is local (loopback), so a
	// locally-monitored Jenkins hangs off the host it runs on instead of floating.
	// The id is host-scoped/tech (never embeds the address), so loopback passes
	// the collapse guard; a remote endpoint yields no edge.
	if rel, ok := entity.LocalRunsOn(entityTypeServiceInstance, targetEntityID, s.serverAddress, s.hostIDFn()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// resolveID returns the pinned id, or "" when the id has not been
// determined yet (caller should emit ok=false this cycle and retry next).
func (s *entitySource) resolveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pinnedID != "" {
		return s.pinnedID
	}

	// Try to fetch the tech id from the target.
	techID, err := s.fetchIdentity()
	if err == nil && techID != "" {
		s.pinnedID = techID
		return s.pinnedID
	}

	// Tech id unavailable; degrade to the host-based fallback and pin it.
	// We pin immediately (not retry) because the controller may have no
	// instance-identity plugin — the fallback is the permanent answer then.
	fallback := s.hostIDFn()
	if fallback != "" {
		s.pinnedID = "jenkins@" + fallback
	} else {
		s.pinnedID = "jenkins"
	}
	s.degraded = true
	return s.pinnedID
}

// fetchInstanceIdentity fetches the Jenkins instance public key fingerprint
// from /instance-identity/api/json and returns it as "jenkins:<hex20>".
// The fingerprint is the first 20 hex characters of the SHA-256 of the
// raw DER bytes of the RSA public key (stable across restarts, unique per
// installation, independent of hostname/port).
func fetchInstanceIdentity(getJSON func(path string, out interface{}) error) (string, error) {
	var resp instanceIdentityResponse
	if err := getJSON("/instance-identity/api/json", &resp); err != nil {
		return "", err
	}
	if resp.PublicKey == "" {
		return "", fmt.Errorf("instance-identity response: empty publicKey")
	}
	der, err := base64.StdEncoding.DecodeString(resp.PublicKey)
	if err != nil {
		return "", fmt.Errorf("instance-identity: decoding publicKey: %w", err)
	}
	sum := sha256.Sum256(der)
	// 20 hex chars = 10 bytes = 80 bits: collision-safe for a service inventory.
	return fmt.Sprintf("jenkins:%x", sum[:10]), nil
}

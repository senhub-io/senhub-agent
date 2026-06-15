package pulsar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// pulsarEntitySource feeds the entity rail with the Apache Pulsar cluster this
// probe monitors. Observe is non-blocking; setReachable is called from Collect.
//
// Entity model (Toise D1 — tech-reported stable id):
//   - service.instance — one per configured cluster endpoint.
//     ID = {service.instance.id: "pulsar:<cluster>"} fetched from the admin API
//     on the first successful collect cycle. "server.address" / "server.port"
//     are descriptive attributes, not part of the identity.
//
// Immutability contract: the id is pinned on the first successful admin-API
// fetch and never changes for the process lifetime. Before it is pinned Observe
// returns ok=false so the detector never emits a placeholder id that a later
// successful fetch would re-key (Toise exact+immutable identity).
type pulsarEntitySource struct {
	// host and port are descriptive; never in the identity key.
	host string
	port int64

	// fetchClusters fetches the list of cluster names from the admin REST API.
	// Injected at construction; tests stub it to avoid live network calls.
	fetchClusters func() ([]string, error)

	// hostID returns the precedence-2 fallback id component ("pulsar@<host.id>"
	// or "pulsar"). Injected at construction; tests stub it.
	hostID func() string

	mu     sync.RWMutex
	up     bool   // last broker readiness result from Collect
	pinned bool   // true once the id has been decided (tech id or fallback)
	id     string // the pinned service.instance.id
}

// entitySourceConfig carries the options the probe passes to the entity source.
type entitySourceConfig struct {
	// instanceName, when non-empty, pins the id immediately (precedence 1 —
	// operator-supplied verbatim override).
	instanceName string
	// endpoint is the admin REST base URL used to derive descriptive host/port
	// and to construct the default cluster-fetch HTTP call.
	endpoint string
	// httpClient is the shared *http.Client from the probe.
	httpClient *http.Client
}

// newPulsarEntitySource builds the entity source from the probe's resolved
// config. When cfg.instanceName is non-empty the id is pinned immediately (no
// lazy fetch needed). Otherwise the id is fetched lazily on the first
// successful Collect cycle.
func newPulsarEntitySource(cfg entitySourceConfig) *pulsarEntitySource {
	host, port := hostPortFromEndpoint(cfg.endpoint)

	s := &pulsarEntitySource{
		host: host,
		port: port,
		fetchClusters: func() ([]string, error) {
			return fetchPulsarClusters(cfg.httpClient, cfg.endpoint)
		},
		hostID: defaultHostID,
	}

	if cfg.instanceName != "" {
		// Precedence 1: operator-supplied name pins immediately at construction.
		s.id = cfg.instanceName
		s.pinned = true
	}

	return s
}

// defaultHostID returns the precedence-2 fallback id "pulsar@<host.id>" using
// the local machine-id. Returns "pulsar" when the machine-id is unavailable.
func defaultHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil || hi.ID == "" {
		return "pulsar"
	}
	return "pulsar@" + hi.ID
}

// fetchPulsarClusters fetches GET <endpoint>/admin/v2/clusters and returns the
// list of cluster names. A non-empty list is sufficient to derive the tech id
// "pulsar:<cluster>" (first cluster name).
func fetchPulsarClusters(client *http.Client, endpoint string) ([]string, error) {
	apiURL := endpoint + "/admin/v2/clusters"
	resp, err := client.Get(apiURL) // #nosec G107 — operator-configured endpoint
	if err != nil {
		return nil, fmt.Errorf("fetching clusters from %s: %w", apiURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching clusters from %s: unexpected status %d", apiURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("reading clusters response: %w", err)
	}
	var clusters []string
	if err := json.Unmarshal(body, &clusters); err != nil {
		return nil, fmt.Errorf("parsing clusters response: %w", err)
	}
	return clusters, nil
}

// setReachable is called from Collect to report the broker readiness result
// for this cycle. When up is true and the id has not yet been pinned, the
// entity source attempts to fetch the cluster name and pin the tech id.
// When up is false on the first call (target never reached), it pins the
// precedence-2 fallback so the entity is not withheld indefinitely.
func (s *pulsarEntitySource) setReachable(up bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up

	if s.pinned {
		return
	}

	if !up {
		// Broker not reachable on this cycle. If the id is not yet pinned at
		// all we pin the fallback so a permanently-unreachable target still
		// eventually gets an entity (after a grace period the caller controls
		// by successive calls). Here we degrade on the very first failure.
		s.id = s.hostID()
		s.pinned = true
		return
	}

	// Broker is reachable: attempt to pin the tech id from the admin API.
	clusters, err := s.fetchClusters()
	if err == nil && len(clusters) > 0 {
		s.id = "pulsar:" + clusters[0]
		s.pinned = true
		return
	}

	// Cluster fetch failed transiently while the broker is reachable. Do not
	// pin a fallback yet — let the next reachable cycle retry. Observe returns
	// ok=false in the meantime so no entity with an unstable id is emitted.
}

// Observe implements entity.Source. It returns the Pulsar service.instance
// entity only once the id has been pinned and the broker is currently up.
// Before the id is pinned it returns (_, false) so the detector keeps the last
// good snapshot rather than emitting a placeholder id that a later successful
// fetch would re-key (Toise immutability). When the broker is transiently
// unreachable after the id is pinned it also returns (_, false) so the detector
// preserves the last known-good topology (audit D3).
func (s *pulsarEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	pinned := s.pinned
	id := s.id
	up := s.up
	s.mu.RUnlock()

	if !pinned || !up {
		return entity.Observation{}, false
	}

	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type: "service.instance",
			ID:   map[string]any{"service.instance.id": id},
			Attributes: map[string]any{
				"service.name":   "pulsar",
				"server.address": s.host,
				"server.port":    s.port,
			},
		}},
	}

	// monitors edge: from the agent's own service.instance to this target.
	// Skipped when the agent id is not yet resolved — an unresolvable From
	// endpoint would be buffered then dropped by the consumer.
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     map[string]any{"service.instance.id": id},
		})
	}

	return obs, true
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 8080 (Pulsar Admin REST API) for http and 8443 for https.
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "localhost", 8080
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return host, 8443
		}
		return host, 8080
	}
	var n int64
	for _, c := range p {
		if c < '0' || c > '9' {
			return host, 8080
		}
		n = n*10 + int64(c-'0')
	}
	return host, n
}

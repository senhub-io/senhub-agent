package pulsar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

	// fetchVersion fetches the broker version from the admin REST API. Returns
	// "" on any error or empty body. Injected at construction; tests stub it.
	fetchVersion func() string

	// hostID returns the precedence-2 fallback id component ("pulsar@<host.id>"
	// or "pulsar"). Injected at construction; tests stub it.
	hostID func() string

	// runsOnHostID is the agent host's raw machine-id, target of the
	// local-target runs_on edge. "" disables the edge.
	runsOnHostID string

	mu      sync.RWMutex
	up      bool   // last broker readiness result from Collect
	pinned  bool   // true once the id has been decided (tech id or fallback)
	id      string // the pinned service.instance.id
	version string // service.version from the admin API, "" until reported
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
		fetchVersion: func() string {
			return fetchPulsarVersion(cfg.httpClient, cfg.endpoint)
		},
		hostID:       defaultHostID,
		runsOnHostID: rawHostID(),
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

// rawHostID returns the agent host's stable machine-id ("" when unreadable),
// used as the runs_on edge target for a locally-monitored broker.
func rawHostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
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

// fetchPulsarVersion fetches GET <endpoint>/admin/v2/brokers/version and returns
// the broker version as a plain-text string (e.g. "3.2.0"). The endpoint is
// already part of the admin REST surface the probe reaches; the version is a
// descriptive attribute so any error or empty body yields "" and the attribute
// is simply omitted from the entity.
func fetchPulsarVersion(client *http.Client, endpoint string) string {
	apiURL := endpoint + "/admin/v2/brokers/version"
	resp, err := client.Get(apiURL) // #nosec G107 — operator-configured endpoint
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
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

	// service.version is descriptive — refresh it on every reachable cycle,
	// independent of id pinning, so an upgrade is reflected without a restart.
	if up && s.fetchVersion != nil {
		if v := s.fetchVersion(); v != "" {
			s.version = v
		}
	}

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
	version := s.version
	s.mu.RUnlock()

	if !pinned || !up {
		return entity.Observation{}, false
	}

	attrs := map[string]any{
		"service.name":   "pulsar",
		"server.address": s.host,
		"server.port":    s.port,
	}
	if version != "" {
		attrs["service.version"] = version
	}

	svcID := map[string]any{"service.instance.id": id}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         svcID,
			Attributes: attrs,
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
			ToID:     svcID,
		})
	}

	// runs_on edge: anchor a locally-monitored broker to the agent host so it
	// does not float with only its monitors edge. The helper's collapse guard
	// suppresses the edge for a remote target or a loopback-derived identity.
	if rel, ok := entity.LocalRunsOn("service.instance", svcID, s.host, s.runsOnHostID); ok {
		obs.Relations = append(obs.Relations, rel)
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

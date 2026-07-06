package solr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// solrEntitySource feeds the entity rail with the Solr instance the probe
// monitors. It exposes the instance as a "db" entity (Toise v0.5.0 strict
// contract) so Toise can inventory it automatically.
//
// Identity precedence (db uniform rule):
//  1. operator config key "instance_name" — verbatim, pinned at construction;
//  2. tech-reported stable id fetched on the first successful collect:
//     SolrCloud cluster id → "solr:<cluster-id>" (from CLUSTERSTATUS);
//  3. host:port — documented db degraded fallback for standalone Solr where
//     no stable tech id exists; pinned at construction, emitted immediately.
//
// Immutability: once pinned, the id NEVER changes for the process lifetime.
// The entity is NOT emitted before the id is pinned (ok=false) so Toise never
// sees a host:port id replaced by the real id later (that would re-key the node).
//
// Exception: when the id source is definitively host:port (standalone path — no
// stable tech id to wait for), the id is pinned at construction and the entity
// is emitted immediately.
type solrEntitySource struct {
	// host and port are parsed from the endpoint URL. They are descriptive
	// attributes and form the host:port fallback id when no tech id is available.
	host string
	port int64
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string

	// client and endpoint are used to fetch the SolrCloud cluster id on the
	// first successful collect. client is the probe's shared HTTP client.
	client   *http.Client
	endpoint string

	mu sync.Mutex

	// instanceID is the pinned db.instance.id. Empty until pinned.
	instanceID string
	// pinned is true once instanceID is set and must not change again.
	pinned bool

	// up and version are updated each collect cycle and gate whether Observe
	// returns the entity.
	up      bool
	version string
}

// newSolrEntitySource builds the entity source from the probe config.
// If instance_name is non-empty, it is used as the db.instance.id immediately
// (pinned at construction). Otherwise, the source attempts to fetch the
// SolrCloud cluster id on the first successful collect; for standalone Solr
// (no stable tech id) it pins host:port at construction.
func newSolrEntitySource(endpoint, instanceName string, client *http.Client) *solrEntitySource {
	addr, port := hostPort(endpoint)
	s := &solrEntitySource{
		host:     addr,
		port:     port,
		hostID:   dbcommon.HostID,
		client:   client,
		endpoint: endpoint,
	}
	if instanceName != "" {
		s.instanceID = instanceName
		s.pinned = true
	}
	return s
}

// setReachable records the current reachability and version string.
// Called from Collect on each cycle; safe for concurrent use.
func (s *solrEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		s.version = version
	}
	s.mu.Unlock()
}

// tryPinClusterIDOrHostPort is the main identity-resolution step called from
// Collect when the instance is reachable and the id is not yet pinned:
//   - first, try SolrCloud CLUSTERSTATUS to get the stable cluster id;
//   - if that fails (standalone Solr or fetch error), fall back to host:port.
//
// This is called with a context that already carries the probe's collect timeout.
// It is a no-op when the id is already pinned.
func (s *solrEntitySource) tryPinClusterIDOrHostPort(ctx context.Context) {
	s.mu.Lock()
	if s.pinned {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	clusterID, err := s.fetchClusterID(ctx)
	if err == nil && clusterID != "" {
		s.mu.Lock()
		if !s.pinned {
			s.instanceID = "solr:" + clusterID
			s.pinned = true
		}
		s.mu.Unlock()
		return
	}
	// Standalone Solr (CLUSTERSTATUS unavailable) or fetch error: pin host:port
	// as the documented db degraded fallback. This is a one-way latch.
	s.mu.Lock()
	if !s.pinned {
		s.instanceID = s.host + ":" + strconv.FormatInt(s.port, 10)
		s.pinned = true
	}
	s.mu.Unlock()
}

// fetchClusterID calls /solr/admin/collections?action=CLUSTERSTATUS&wt=json
// and extracts the cluster.properties.id field. Returns ("", err) when the
// endpoint is not available (standalone Solr) or the id field is absent.
func (s *solrEntitySource) fetchClusterID(ctx context.Context) (string, error) {
	rawURL := s.endpoint + "/solr/admin/collections?action=CLUSTERSTATUS&wt=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("building CLUSTERSTATUS request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET CLUSTERSTATUS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return "", fmt.Errorf("reading CLUSTERSTATUS response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CLUSTERSTATUS returned HTTP %d", resp.StatusCode)
	}

	var r clusterStatusResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parsing CLUSTERSTATUS: %w", err)
	}
	id, _ := r.Cluster.Properties["id"].(string)
	return id, nil
}

// clusterStatusResponse is the relevant subset of the CLUSTERSTATUS response.
// Only cluster.properties.id is consumed here.
type clusterStatusResponse struct {
	Cluster struct {
		Properties map[string]any `json:"properties"`
	} `json:"cluster"`
}

// Observe implements entity.Source. It returns the current Solr db entity
// when the instance was reachable on the last collection cycle AND the
// instance id has been pinned. The monitors edge (agent → db) is appended
// when the agent instance id is available.
//
// Returning (obs, false) means "transient failure — keep the last good
// state": the detector's staleness TTL handles expiry.
func (s *solrEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.up || !s.pinned {
		return entity.Observation{}, false
	}

	attrs := map[string]any{
		"db.system.name": "solr",
		"server.address": s.host,
		"server.port":    s.port,
	}
	if s.version != "" {
		attrs["db.system.version"] = s.version
	}

	dbID := map[string]any{"db.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         dbID,
			Attributes: attrs,
		}},
	}

	agentID := agentstate.GetAgentInstanceID()
	if agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     dbID,
		})
	}

	// runs_on edge: db → host when the db is on the agent's own host (loopback).
	// The collapse guard suppresses it for a host:port-derived id.
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.host, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// hostPort parses an endpoint URL (e.g. "http://localhost:8983") and returns
// the host and port as scalars suitable for descriptive attributes and the
// fallback id. When the URL cannot be parsed or has no explicit port, sensible
// defaults are returned.
func hostPort(endpoint string) (string, int64) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return "localhost", 8983
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		switch u.Scheme {
		case "https":
			return host, 443
		default:
			return host, 8983
		}
	}
	var port int64
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return host, 8983
		}
		port = port*10 + int64(c-'0')
	}
	return host, port
}

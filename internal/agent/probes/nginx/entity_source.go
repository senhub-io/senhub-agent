package nginx

import (
	"net/url"
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// nginxEntitySource feeds the entity rail with the nginx instance this probe
// monitors. It reports a single service.instance entity with a stable,
// non-network-derived identity. The entity is emitted only while the
// stub_status page is reachable (up=true); a transient fetch failure returns
// ok=false so the tracker reuses the last good snapshot rather than emitting a
// delete.
type nginxEntitySource struct {
	instanceID string
	attrs      map[string]any
	mu         sync.RWMutex
	up         bool
	version    string // service.version from the Server header, "" until parsed
}

// newNginxEntitySource constructs the entity source. instanceName is the
// operator-configured "instance_name" override; hostID is the agent's stable
// host identity (common.GetHostIdentity().ID). The endpoint URL is parsed only
// for descriptive server.address / server.port attributes — it never
// participates in the identity.
func newNginxEntitySource(endpoint, instanceName, hostID string) *nginxEntitySource {
	var id string
	switch {
	case instanceName != "":
		id = instanceName
	case hostID != "":
		id = "nginx@" + hostID
	default:
		id = "nginx"
	}

	host, port := hostPortFromEndpoint(endpoint)
	return &nginxEntitySource{
		instanceID: id,
		attrs: map[string]any{
			"service.name":   "nginx",
			"server.address": host,
			"server.port":    port,
		},
	}
}

// hostPortFromEndpoint extracts host and port (as int64) from an HTTP(S) URL.
// Missing port defaults to 80 for http and 443 for https.
func hostPortFromEndpoint(rawURL string) (host string, port int64) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return rawURL, 80
	}
	host = u.Hostname()
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return host, 443
		}
		return host, 80
	}
	var n int64
	for _, c := range p {
		if c < '0' || c > '9' {
			return host, 80
		}
		n = n*10 + int64(c-'0')
	}
	return host, n
}

// setReachable is called from Collect to report whether the stub_status page
// responded successfully this cycle.
func (s *nginxEntitySource) setReachable(up bool) {
	s.mu.Lock()
	s.up = up
	s.mu.Unlock()
}

// setVersion records the server version parsed from the stub_status response's
// Server header ("nginx/1.27.0" → "1.27.0"), so it rides the entity as the
// descriptive service.version attribute (toise#216 AT1). Empty values are
// ignored (server_tokens off hides the version).
func (s *nginxEntitySource) setVersion(v string) {
	if v == "" {
		return
	}
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// Observe implements entity.Source. It returns the nginx service.instance
// entity plus a monitors relation from the agent when the endpoint is
// reachable, or (_, false) on a transient failure so the detector keeps the
// last good snapshot alive.
func (s *nginxEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	up := s.up
	version := s.version
	s.mu.RUnlock()

	if !up {
		return entity.Observation{}, false
	}

	attrs := s.attrs
	if version != "" {
		// Merge into a fresh map so the shared attrs is never mutated.
		attrs = make(map[string]any, len(s.attrs)+1)
		for k, v := range s.attrs {
			attrs[k] = v
		}
		attrs["service.version"] = version
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": s.instanceID},
			Attributes: attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "service.instance",
			ToID:     map[string]any{"service.instance.id": s.instanceID},
		})
	}

	return obs, true
}

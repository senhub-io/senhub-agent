package influxdb

import (
	"net"
	"net/url"
	"strconv"
	"sync"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// influxdbEntitySource feeds the entity rail with the monitored InfluxDB
// instance as a "db" entity (Toise contract). Observe is non-blocking;
// the probe updates reachability and version on each Collect cycle.
//
// Identity rule — InfluxDB exposes no stable server UUID over the API this
// probe uses, so the id degrades gracefully:
//
//  1. operator-supplied "instance_name" config key (verbatim, pinned at construction);
//  2. host:port derived from the configured endpoint (the documented db
//     degraded fallback when no stable tech id exists).
//
// Both paths pin the id at construction time and never change it, which
// satisfies Toise's immutability contract (a changing id re-keys the db in
// the consumer).
type influxdbEntitySource struct {
	instanceID string // pinned at construction, never changes
	host       string
	port       int64
	// hostID resolves the agent host id for a local-db runs_on edge.
	// nil → dbcommon.HostID.
	hostID func() string
	// rekey announces the 0.5.4 identity migration for a local instance whose
	// id was host-scoped. nil when nothing was re-keyed (remote target).
	rekey *entity.RekeyAnnouncer

	mu    sync.RWMutex
	up    bool
	attrs map[string]any
}

// newInfluxdbEntitySource builds the entity source from the probe's config.
// The instance ID is chosen once and is immutable for the process lifetime:
// instance_name (if set) takes precedence; otherwise host:port is used as
// the documented db fallback.
func newInfluxdbEntitySource(cfg probeConfig) *influxdbEntitySource {
	addr, portStr := endpointParts(cfg.Endpoint)
	port, _ := strconv.ParseInt(portStr, 10, 64)

	var instanceID string
	// The announcer is built only when the fallback applies: an operator-named
	// instance was never keyed on address:port, so there is no node to retire.
	var rekey *entity.RekeyAnnouncer
	if cfg.InstanceName != "" {
		instanceID = cfg.InstanceName
	} else {
		instanceID = dbcommon.FallbackInstanceID("influxdb", addr, int(port), dbcommon.HostID())
		rekey = dbcommon.NewRekeyAnnouncer("influxdb", addr, int(port), dbcommon.HostID())
	}

	return &influxdbEntitySource{
		instanceID: instanceID,
		host:       addr,
		port:       port,
		hostID:     dbcommon.HostID,
		rekey:      rekey,
	}
}

// setReachable updates the liveness state and version banner.
// Called from Collect after the /health check.
func (s *influxdbEntitySource) setReachable(up bool, version string) {
	s.mu.Lock()
	s.up = up
	if up {
		s.attrs = map[string]any{
			"db.system.name": "influxdb",
			"server.address": s.host,
			"server.port":    s.port,
		}
		if version != "" {
			s.attrs["db.system.version"] = version
		}
	} else {
		s.attrs = nil
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns the db entity plus a monitors
// edge from the agent when the instance is reachable. Returning ok=false when
// down preserves the consumer's last good snapshot instead of treating the
// instance as deleted on a transient failure.
func (s *influxdbEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}

	dbID := map[string]any{"db.instance.id": s.instanceID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "db",
			ID:         dbID,
			Attributes: s.attrs,
		}},
	}

	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   "db",
			ToID:     map[string]any{"db.instance.id": s.instanceID},
		})
	}

	// runs_on edge: db → host when the db is on the agent's own host (loopback).
	// The collapse guard suppresses it for a host:port-derived id (no tech id).
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, s.host, s.hostID()); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	// The 0.5.4 identity migration: retire the pre-scoping node explicitly and
	// alias it to this one, so the consumer reads "someone decided" rather than
	// "the agent went quiet". Self-limiting to a few cycles.
	s.rekey.Announce()
	if rel, ok := s.rekey.SameAs(); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

// endpointParts extracts the host and port from an InfluxDB endpoint URL.
// Falls back to the host portion of the URL and the default InfluxDB port
// when the URL carries no explicit port.
func endpointParts(endpoint string) (host, port string) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint, "8086"
	}
	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		// No explicit port — use the default InfluxDB port.
		return u.Hostname(), "8086"
	}
	return h, p
}

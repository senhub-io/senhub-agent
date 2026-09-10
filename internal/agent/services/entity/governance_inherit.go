package entity

import "strings"

// locationKeys are the governance attributes that describe where a thing
// physically is. Anything that runs on this host is where the host is, so
// these four keys, and only these, descend from the host's governance to
// the entities placed on it. Owner, criticality, lifecycle and labels do
// not: a database on a machine is often another team's, and a host runs
// more than one application.
var locationKeys = [...]string{
	"entity.location.site",
	"entity.location.datacenter",
	"entity.location.rack",
	"entity.location.room",
}

// ownedTypes are the entity types that inherit the host's ownership. An
// operator reasons about a service, a container, a database, a device;
// they do not reason about a listening port, and a listener reaches its
// host in one `runs_on` hop, so the answer to "who owns this port" is
// already a traversal away. Stamping every listener would copy a fact
// the graph already holds, and multiply the entities carrying it.
var ownedTypes = map[string]bool{
	"service.instance": true,
	"container":        true,
	"db":               true,
	"network.device":   true,
}

// ownershipKeys are the governance attributes that say who answers for a
// thing. Unlike location they do not describe a place, so they descend
// only to the types above.
var ownershipKeys = [...]string{
	"entity.owner.team",
	"entity.owner.contact",
	"service.criticality",
	"entity.lifecycle.status",
}

// labelPrefix marks the operator's own labels, of which the head of an
// application chain is one. They travel with ownership.
const labelPrefix = "entity.label."

// inheritHostGovernance stamps the host's governance onto the entities of
// obs that a `runs_on` relation places on this host, on the keys the
// entity does not already set. Two rules, because the two halves of the
// vocabulary do not mean the same thing: what runs on this host is where
// the host is, so the location descends to everything local; who answers
// for it is a different question, so ownership descends only to the types
// an operator reasons about.
//
// Entities without that relation, the remote systems a collector-mode
// probe reads among them, are left alone: a database on another machine
// is not owned by the team that owns the collector, and saying so would
// be wrong rather than merely noisy. Observed maps are never mutated;
// sources may cache them.
func inheritHostGovernance(obs Observation, hostID string, hostAttrs map[string]any) Observation {
	if hostID == "" {
		return obs
	}
	loc := make(map[string]any, len(locationKeys))
	for _, k := range locationKeys {
		if v, ok := hostAttrs[k]; ok && v != "" {
			loc[k] = v
		}
	}
	owner := map[string]any{}
	for _, k := range ownershipKeys {
		if v, ok := hostAttrs[k]; ok && v != "" {
			owner[k] = v
		}
	}
	for k, v := range hostAttrs {
		if strings.HasPrefix(k, labelPrefix) && v != "" {
			owner[k] = v
		}
	}
	if len(loc) == 0 && len(owner) == 0 {
		return obs
	}
	local := map[string]bool{}
	for _, r := range obs.Relations {
		if r.Type == "runs_on" && r.ToType == "host" && r.ToID["host.id"] == hostID {
			local[entityKey(r.FromType, r.FromID)] = true
		}
	}
	if len(local) == 0 {
		return obs
	}
	entities := make([]Entity, len(obs.Entities))
	copy(entities, obs.Entities)
	for i := range entities {
		e := &entities[i]
		if e.Type == "host" || !local[entityKey(e.Type, e.ID)] {
			continue
		}
		merged := make(map[string]any, len(e.Attributes)+len(loc)+len(owner))
		for k, v := range e.Attributes {
			merged[k] = v
		}
		// The source's own word wins: a probe that knows the owner of what
		// it reports knows better than the host it happens to run on.
		for k, v := range loc {
			if _, set := merged[k]; !set {
				merged[k] = v
			}
		}
		if ownedTypes[e.Type] {
			for k, v := range owner {
				if _, set := merged[k]; !set {
					merged[k] = v
				}
			}
		}
		e.Attributes = merged
	}
	obs.Entities = entities
	return obs
}

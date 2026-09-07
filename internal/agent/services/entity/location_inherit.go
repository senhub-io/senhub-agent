package entity

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

// inheritHostLocation stamps the host's location onto every entity of obs
// that a `runs_on` relation places on this host, on the keys the entity
// does not already set. Entities without that relation, remote systems a
// collector-mode probe reads among them, are left alone. Observed maps are
// never mutated; sources may cache them.
func inheritHostLocation(obs Observation, hostID string, hostAttrs map[string]any) Observation {
	if hostID == "" {
		return obs
	}
	loc := make(map[string]any, len(locationKeys))
	for _, k := range locationKeys {
		if v, ok := hostAttrs[k]; ok && v != "" {
			loc[k] = v
		}
	}
	if len(loc) == 0 {
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
		merged := make(map[string]any, len(e.Attributes)+len(loc))
		for k, v := range e.Attributes {
			merged[k] = v
		}
		for k, v := range loc {
			if _, set := merged[k]; !set {
				merged[k] = v
			}
		}
		e.Attributes = merged
	}
	obs.Entities = entities
	return obs
}

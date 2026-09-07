package entity

// WithAttributes wraps a source so every entity it observes carries attrs,
// on the keys the entity does not already set. The source's own attribute
// is always the more specific statement (snmp_poll stamps a discovery rule
// per device), so it wins on a collision. A host entity is left alone: the
// host belongs to the agent-level governance, not to what one instance
// observes. Observed maps are never mutated; the source may cache them.
func WithAttributes(src Source, attrs map[string]any) Source {
	if src == nil || len(attrs) == 0 {
		return src
	}
	return attributedSource{src: src, attrs: attrs}
}

type attributedSource struct {
	src   Source
	attrs map[string]any
}

func (a attributedSource) Observe() (Observation, bool) {
	o, ok := a.src.Observe()
	if !ok || len(o.Entities) == 0 {
		return o, ok
	}
	entities := make([]Entity, len(o.Entities))
	copy(entities, o.Entities)
	for i := range entities {
		if entities[i].Type == "host" {
			continue
		}
		merged := make(map[string]any, len(entities[i].Attributes)+len(a.attrs))
		for k, v := range entities[i].Attributes {
			merged[k] = v
		}
		for k, v := range a.attrs {
			if _, set := merged[k]; !set {
				merged[k] = v
			}
		}
		entities[i].Attributes = merged
	}
	o.Entities = entities
	return o, ok
}

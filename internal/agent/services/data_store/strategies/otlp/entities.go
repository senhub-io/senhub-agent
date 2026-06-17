package otlp

import (
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/log"

	"senhub-agent.go/internal/agent/services/entity"
)

// Entity-event wire encoding, frozen with Toise on the merged OTel
// entity-events spec (#222). A node event's kind is the LogRecord
// EventName (entity.state / entity.delete); node attributes use bare keys
// (entity.type / entity.id / entity.description / entity.report.interval),
// no otel.entity.* prefix and no event-type payload attribute.
//
// Edges are embedded in the source entity's state via entity.relationships: an
// array of bare descriptors {relationship.type, entity.type, entity.id} naming
// the target only (the source is the carrying entity). There are no separate
// relation records and no edge delete — a relation a heartbeat stops listing
// is retired by absence.
const (
	eventNameEntityState  = "entity.state"
	eventNameEntityDelete = "entity.delete"

	attrEntityType           = "entity.type"
	attrEntityID             = "entity.id"
	attrEntityDescription    = "entity.description"
	attrEntityReportInterval = "entity.report.interval"

	attrEntityRelationships = "entity.relationships"
	attrRelationshipType    = "relationship.type"
)

// buildEntityRecord encodes a neutral entity.Event into the OTel log Record
// carried on the OTLP log signal. The record's Timestamp is the producer
// observation time (becomes the consumer's event_time); identity and
// attributes are self-contained record attributes (NOT resource-referenced)
// so the consumer reads otel.entity.id straight off the record.
//
// Attribute maps are flat with scalar leaves only; a non-scalar leaf is an
// error, never a silent drop. Keys are sorted for deterministic output.
func buildEntityRecord(ev entity.Event) (log.Record, error) {
	var rec log.Record
	rec.SetTimestamp(ev.Time)
	rec.SetObservedTimestamp(ev.Time)

	var attrs []log.KeyValue
	switch ev.Kind {
	case entity.EntityState, entity.EntityDelete:
		e := ev.Entity
		if e == nil {
			return rec, fmt.Errorf("entity event kind %d has nil Entity", ev.Kind)
		}
		// The event kind is the LogRecord EventName, not a payload attribute.
		if ev.Kind == entity.EntityDelete {
			rec.SetEventName(eventNameEntityDelete)
		} else {
			rec.SetEventName(eventNameEntityState)
		}
		id, err := scalarMap(attrEntityID, e.ID)
		if err != nil {
			return rec, err
		}
		// type AND id are required on both state and delete.
		attrs = []log.KeyValue{
			log.String(attrEntityType, e.Type),
			id,
		}
		if ev.Kind == entity.EntityState && len(e.Attributes) > 0 {
			a, err := scalarMap(attrEntityDescription, e.Attributes)
			if err != nil {
				return rec, err
			}
			attrs = append(attrs, a)
		}
		// Liveness backstop: the heartbeat validity window. The consumer
		// arms a deadline (last_seen + interval) and expires the entity if
		// no heartbeat or explicit delete arrives — covers producers that
		// die without a clean delete (kill -9, partition). Emitted on state
		// only, in SECONDS per the merged spec; a delete needs no interval.
		if ev.Kind == entity.EntityState && ev.Interval > 0 {
			attrs = append(attrs, log.Int64(attrEntityReportInterval, int64(ev.Interval.Seconds())))
		}
		// Embedded outgoing edges. State only — a delete retires the whole
		// node, relationships included. The set is full each heartbeat;
		// removal is by absence (no edge delete on the wire).
		if ev.Kind == entity.EntityState && len(e.Relationships) > 0 {
			rels, err := relationshipsValue(e.Relationships)
			if err != nil {
				return rec, err
			}
			attrs = append(attrs, rels)
		}

	default:
		return rec, fmt.Errorf("unknown entity event kind %d", ev.Kind)
	}

	rec.AddAttributes(attrs...)
	return rec, nil
}

// relationshipsValue encodes the embedded entity.relationships array: one bare
// descriptor per outgoing edge, in producer order (the consumer treats it as a
// set, so order is not significant).
func relationshipsValue(rels []entity.Relationship) (log.KeyValue, error) {
	vals := make([]log.Value, 0, len(rels))
	for _, rel := range rels {
		idKVs, err := scalarKVs(rel.TargetID)
		if err != nil {
			return log.KeyValue{}, fmt.Errorf("%s[%s→%s]: %w", attrEntityRelationships, rel.Type, rel.TargetType, err)
		}
		vals = append(vals, log.MapValue(
			log.String(attrRelationshipType, rel.Type),
			log.String(attrEntityType, rel.TargetType),
			log.Map(attrEntityID, idKVs...),
		))
	}
	return log.Slice(attrEntityRelationships, vals...), nil
}

// scalarMap builds a kvlist log attribute from a flat map of scalar values,
// keys sorted for deterministic output.
func scalarMap(key string, m map[string]any) (log.KeyValue, error) {
	kvs, err := scalarKVs(m)
	if err != nil {
		return log.KeyValue{}, fmt.Errorf("%s%w", key, err)
	}
	return log.Map(key, kvs...), nil
}

// scalarKVs renders a flat map of scalar values to sorted log.KeyValues. The
// returned error is prefixed with [key] so a caller can name the field.
func scalarKVs(m map[string]any) ([]log.KeyValue, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	kvs := make([]log.KeyValue, 0, len(keys))
	for _, k := range keys {
		kv, err := scalarKV(k, m[k])
		if err != nil {
			return nil, fmt.Errorf("[%s]: %w", k, err)
		}
		kvs = append(kvs, kv)
	}
	return kvs, nil
}

// scalarKV converts a single scalar value to a log.KeyValue. Only the four
// scalar kinds the entity contract allows are accepted; anything else (a
// slice, a nested map) is an error so it surfaces rather than being dropped.
func scalarKV(k string, v any) (log.KeyValue, error) {
	switch t := v.(type) {
	case string:
		return log.String(k, t), nil
	case int64:
		return log.Int64(k, t), nil
	case int:
		return log.Int64(k, int64(t)), nil
	case float64:
		return log.Float64(k, t), nil
	case bool:
		return log.Bool(k, t), nil
	default:
		return log.KeyValue{}, fmt.Errorf("non-scalar value of type %T", v)
	}
}

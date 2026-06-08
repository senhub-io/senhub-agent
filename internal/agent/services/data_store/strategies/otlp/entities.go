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
// Edges still use the entity.relation.* extension here — the embedded
// entity.relationships form is the next lot (#222 lot 0b); until then a
// relation is a separate record a strict-spec consumer ignores.
const (
	eventNameEntityState  = "entity.state"
	eventNameEntityDelete = "entity.delete"

	attrEntityType           = "entity.type"
	attrEntityID             = "entity.id"
	attrEntityDescription    = "entity.description"
	attrEntityReportInterval = "entity.report.interval"

	attrRelEventType = "entity.relation.event.type"
	attrRelType      = "entity.relation.type"
	attrRelFromType  = "entity.relation.from.type"
	attrRelFromID    = "entity.relation.from.id"
	attrRelToType    = "entity.relation.to.type"
	attrRelToID      = "entity.relation.to.id"
	attrRelAttrs     = "entity.relation.attributes"

	relationEventStateValue  = "state"
	relationEventDeleteValue = "delete"
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

	case entity.RelationState, entity.RelationDelete:
		r := ev.Relation
		if r == nil {
			return rec, fmt.Errorf("relation event kind %d has nil Relation", ev.Kind)
		}
		eventType := relationEventStateValue
		if ev.Kind == entity.RelationDelete {
			eventType = relationEventDeleteValue
		}
		from, err := scalarMap(attrRelFromID, r.FromID)
		if err != nil {
			return rec, err
		}
		to, err := scalarMap(attrRelToID, r.ToID)
		if err != nil {
			return rec, err
		}
		attrs = []log.KeyValue{
			log.String(attrRelEventType, eventType),
			log.String(attrRelType, r.Type),
			log.String(attrRelFromType, r.FromType),
			from,
			log.String(attrRelToType, r.ToType),
			to,
		}
		if ev.Kind == entity.RelationState && len(r.Attributes) > 0 {
			a, err := scalarMap(attrRelAttrs, r.Attributes)
			if err != nil {
				return rec, err
			}
			attrs = append(attrs, a)
		}

	default:
		return rec, fmt.Errorf("unknown entity event kind %d", ev.Kind)
	}

	rec.AddAttributes(attrs...)
	return rec, nil
}

// scalarMap builds a kvlist log attribute from a flat map of scalar values,
// keys sorted for deterministic output.
func scalarMap(key string, m map[string]any) (log.KeyValue, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	kvs := make([]log.KeyValue, 0, len(keys))
	for _, k := range keys {
		kv, err := scalarKV(k, m[k])
		if err != nil {
			return log.KeyValue{}, fmt.Errorf("%s[%s]: %w", key, k, err)
		}
		kvs = append(kvs, kv)
	}
	return log.Map(key, kvs...), nil
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

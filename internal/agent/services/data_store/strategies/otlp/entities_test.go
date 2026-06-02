package otlp

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"

	"senhub-agent.go/internal/agent/services/entity"
)

// recordAttrs flattens a log.Record's attributes into a comparable
// map[string]any, recursing into kvlist (map) values. Used to assert the
// emitted wire shape against the frozen Toise contract.
func recordAttrs(rec log.Record) map[string]any {
	out := map[string]any{}
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		out[kv.Key] = logValueToAny(kv.Value)
		return true
	})
	return out
}

func logValueToAny(v log.Value) any {
	switch v.Kind() {
	case log.KindString:
		return v.AsString()
	case log.KindInt64:
		return v.AsInt64()
	case log.KindFloat64:
		return v.AsFloat64()
	case log.KindBool:
		return v.AsBool()
	case log.KindMap:
		m := map[string]any{}
		for _, kv := range v.AsMap() {
			m[kv.Key] = logValueToAny(kv.Value)
		}
		return m
	default:
		return nil
	}
}

// TestBuildEntityRecord_Foundation pins the Lot 1 wire shapes (host +
// service.instance entities + the runs_on relation) against the agreed
// Toise entity-events contract.
func TestBuildEntityRecord_Foundation(t *testing.T) {
	now := time.Unix(1780272000, 0).UTC()
	h := entity.HostIdentity{ID: "h-001", Name: "web-server-1", OSType: "linux"}
	a := entity.AgentIdentity{InstanceID: "agent-7f3a", ServiceName: "senhub-agent", ServiceVersion: "1.0.0"}

	events := entity.DetectFoundation(h, a, now, time.Minute)
	if len(events) != 3 {
		t.Fatalf("DetectFoundation: got %d events, want 3", len(events))
	}

	cases := []struct {
		name string
		ev   entity.Event
		want map[string]any
	}{
		{
			name: "host",
			ev:   events[0],
			want: map[string]any{
				"otel.entity.event.type": "entity_state",
				"otel.entity.type":       "host",
				"otel.entity.id":         map[string]any{"host.id": "h-001"},
				"otel.entity.attributes": map[string]any{"host.name": "web-server-1", "os.type": "linux"},
			},
		},
		{
			name: "service.instance",
			ev:   events[1],
			want: map[string]any{
				"otel.entity.event.type": "entity_state",
				"otel.entity.type":       "service.instance",
				"otel.entity.id":         map[string]any{"service.instance.id": "agent-7f3a"},
				"otel.entity.attributes": map[string]any{"service.name": "senhub-agent", "service.version": "1.0.0"},
			},
		},
		{
			name: "runs_on",
			ev:   events[2],
			want: map[string]any{
				"entity.relation.event.type": "state",
				"entity.relation.type":       "runs_on",
				"entity.relation.from.type":  "service.instance",
				"entity.relation.from.id":    map[string]any{"service.instance.id": "agent-7f3a"},
				"entity.relation.to.type":    "host",
				"entity.relation.to.id":      map[string]any{"host.id": "h-001"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := buildEntityRecord(tc.ev)
			if err != nil {
				t.Fatalf("buildEntityRecord: %v", err)
			}
			if !rec.Timestamp().Equal(now) {
				t.Errorf("timestamp = %v, want %v (event_time must be the observation instant)", rec.Timestamp(), now)
			}
			got := recordAttrs(rec)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("wire shape mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// TestBuildEntityRecord_RelationPurity asserts a relation record carries no
// otel.entity.* attribute, so a standard OTel entity-event consumer ignores
// it cleanly rather than seeing a malformed entity event.
func TestBuildEntityRecord_RelationPurity(t *testing.T) {
	now := time.Unix(1780272180, 0).UTC()
	rel := entity.Event{
		Kind: entity.RelationState,
		Time: now,
		Relation: &entity.Relation{
			Type:     "runs_on",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": "agent-7f3a"},
			ToType:   "host",
			ToID:     map[string]any{"host.id": "h-001"},
		},
	}
	rec, err := buildEntityRecord(rel)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	for k := range recordAttrs(rec) {
		if strings.HasPrefix(k, "otel.entity.") {
			t.Errorf("relation record carries forbidden otel.entity.* attribute %q", k)
		}
	}
}

// TestBuildEntityRecord_DeleteCarriesTypeAndID asserts entity_delete still
// carries both otel.entity.type and otel.entity.id (both required on delete).
func TestBuildEntityRecord_DeleteCarriesTypeAndID(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityDelete,
		Time: time.Unix(1780272420, 0).UTC(),
		Entity: &entity.Entity{
			Type:       "db",
			ID:         map[string]any{"db.instance.id": "pg@10.0.1.5:5432"},
			Attributes: map[string]any{"should": "be ignored on delete"},
		},
	}
	rec, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	got := recordAttrs(rec)
	want := map[string]any{
		"otel.entity.event.type": "entity_delete",
		"otel.entity.type":       "db",
		"otel.entity.id":         map[string]any{"db.instance.id": "pg@10.0.1.5:5432"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("delete wire shape mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

// TestBuildEntityRecord_TypedScalars asserts non-string scalar leaves keep
// their type (int64 stays int64, not stringified) — needed for db
// descriptive attributes like server.port.
func TestBuildEntityRecord_TypedScalars(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1780272120, 0).UTC(),
		Entity: &entity.Entity{
			Type: "db",
			ID:   map[string]any{"db.instance.id": "pg@10.0.1.5:5432"},
			Attributes: map[string]any{
				"db.system.name": "postgresql",
				"server.address": "10.0.1.5",
				"server.port":    int64(5432),
			},
		},
	}
	rec, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	attrs := recordAttrs(rec)["otel.entity.attributes"].(map[string]any)
	if got, ok := attrs["server.port"].(int64); !ok || got != 5432 {
		t.Errorf("server.port = %#v, want int64(5432)", attrs["server.port"])
	}
}

// TestBuildEntityRecord_RejectsNonScalar asserts a non-scalar leaf is an
// error, never silently dropped (our side of the no-silent-loss contract).
func TestBuildEntityRecord_RejectsNonScalar(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1780272000, 0).UTC(),
		Entity: &entity.Entity{
			Type:       "host",
			ID:         map[string]any{"host.id": "h-001"},
			Attributes: map[string]any{"addresses": []string{"10.0.0.1", "10.0.0.2"}},
		},
	}
	if _, err := buildEntityRecord(ev); err == nil {
		t.Fatal("expected error for non-scalar attribute value, got nil")
	}
}

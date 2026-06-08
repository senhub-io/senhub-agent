package otlp

import (
	"reflect"
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
	case log.KindSlice:
		s := []any{}
		for _, e := range v.AsSlice() {
			s = append(s, logValueToAny(e))
		}
		return s
	default:
		return nil
	}
}

// TestBuildEntityRecord_Foundation pins the Lot 1 wire shapes against the
// agreed Toise entity-events contract: host (no edges) and service.instance
// carrying the runs_on edge embedded in entity.relationships (the folded
// form, matching conformance records #0 and #1). The entities here are built
// in their post-fold shape; fold correctness is covered in the entity package.
func TestBuildEntityRecord_Foundation(t *testing.T) {
	now := time.Unix(1780272000, 0).UTC()

	cases := []struct {
		name   string
		entity entity.Entity
		want   map[string]any
	}{
		{
			name: "host",
			entity: entity.Entity{
				Type:       "host",
				ID:         map[string]any{"host.id": "h-001"},
				Attributes: map[string]any{"host.name": "web-server-1", "os.type": "linux"},
			},
			want: map[string]any{
				"entity.type":            "host",
				"entity.id":              map[string]any{"host.id": "h-001"},
				"entity.description":     map[string]any{"host.name": "web-server-1", "os.type": "linux"},
				"entity.report.interval": int64(60),
			},
		},
		{
			name: "service.instance",
			entity: entity.Entity{
				Type:       "service.instance",
				ID:         map[string]any{"service.instance.id": "agent-7f3a"},
				Attributes: map[string]any{"service.name": "senhub-agent", "service.version": "1.0.0"},
				Relationships: []entity.Relationship{
					{Type: "runs_on", TargetType: "host", TargetID: map[string]any{"host.id": "h-001"}},
				},
			},
			want: map[string]any{
				"entity.type":            "service.instance",
				"entity.id":              map[string]any{"service.instance.id": "agent-7f3a"},
				"entity.description":     map[string]any{"service.name": "senhub-agent", "service.version": "1.0.0"},
				"entity.report.interval": int64(60),
				"entity.relationships": []any{
					map[string]any{
						"relationship.type": "runs_on",
						"entity.type":       "host",
						"entity.id":         map[string]any{"host.id": "h-001"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.entity
			ev := entity.Event{Kind: entity.EntityState, Entity: &e, Time: now, Interval: time.Minute}
			rec, err := buildEntityRecord(ev)
			if err != nil {
				t.Fatalf("buildEntityRecord: %v", err)
			}
			if !rec.Timestamp().Equal(now) {
				t.Errorf("timestamp = %v, want %v (event_time must be the observation instant)", rec.Timestamp(), now)
			}
			if rec.EventName() != "entity.state" {
				t.Errorf("EventName = %q, want entity.state", rec.EventName())
			}
			got := recordAttrs(rec)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("wire shape mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// TestBuildEntityRecord_EmbeddedRelationships pins the entity.relationships
// array shape — multiple bare descriptors {relationship.type, entity.type,
// entity.id} on one entity.state record — against the Toise contract (record
// #3: service.instance with runs_on + monitors).
func TestBuildEntityRecord_EmbeddedRelationships(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1780272060, 0).UTC(),
		Entity: &entity.Entity{
			Type:       "service.instance",
			ID:         map[string]any{"service.instance.id": "agent-7f3a"},
			Attributes: map[string]any{"service.name": "senhub-agent", "service.version": "1.0.0"},
			Relationships: []entity.Relationship{
				{Type: "runs_on", TargetType: "host", TargetID: map[string]any{"host.id": "h-001"}},
				{Type: "monitors", TargetType: "db", TargetID: map[string]any{"db.instance.id": "postgresql:7311168095704935424"}},
			},
		},
	}
	rec, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	got := recordAttrs(rec)["entity.relationships"]
	want := []any{
		map[string]any{
			"relationship.type": "runs_on",
			"entity.type":       "host",
			"entity.id":         map[string]any{"host.id": "h-001"},
		},
		map[string]any{
			"relationship.type": "monitors",
			"entity.type":       "db",
			"entity.id":         map[string]any{"db.instance.id": "postgresql:7311168095704935424"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entity.relationships mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

// TestBuildEntityRecord_DeleteCarriesTypeAndID asserts entity.delete uses the
// entity.delete EventName and still carries both entity.type and entity.id
// (both required on delete), with no description.
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
	if rec.EventName() != "entity.delete" {
		t.Errorf("EventName = %q, want entity.delete", rec.EventName())
	}
	got := recordAttrs(rec)
	want := map[string]any{
		"entity.type": "db",
		"entity.id":   map[string]any{"db.instance.id": "pg@10.0.1.5:5432"},
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
	attrs := recordAttrs(rec)["entity.description"].(map[string]any)
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

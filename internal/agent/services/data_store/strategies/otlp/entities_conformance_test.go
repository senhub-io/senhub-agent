package otlp

import (
	"testing"
	"time"

	"github.com/toise-dev/toise/pkg/emit"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/otel/log"

	"senhub-agent.go/internal/agent/services/entity"
)

// Differential conformance against the Toise producer SDK (#455).
//
// The agent hand-rolls the entity-event wire encoding; toise-emit is the
// consumer-side reference for the same contract, with a conformance kit that
// pins the byte form. Rather than take a runtime dependency on the SDK —
// which would mean a second export path outside this strategy's batching,
// backpressure and tenant headers — the encoders are compared here. Drift
// between producer and consumer then breaks the build, which is the whole
// value #455 asks for, without coupling the runtime.
//
// The dependency is test-scope: nothing under emit is imported by
// non-test code, so it is not linked into the shipped binary.

// buildEmitReference renders the same observation through the SDK.
// grpc.NewClient is lazy, so New dials nothing and Build needs no server.
func buildEmitReference(t *testing.T, eventName string, e emit.Entity) plog.LogRecord {
	t.Helper()
	c, err := emit.New(emit.Options{Endpoint: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("emit.New: %v", err)
	}
	defer c.Close()

	ld, err := c.Build(eventName, []emit.Entity{e})
	if err != nil {
		t.Fatalf("emit.Build: %v", err)
	}
	rl := ld.ResourceLogs()
	if rl.Len() != 1 {
		t.Fatalf("expected 1 ResourceLogs, got %d", rl.Len())
	}
	sl := rl.At(0).ScopeLogs()
	if sl.Len() != 1 {
		t.Fatalf("expected 1 ScopeLogs, got %d", sl.Len())
	}
	recs := sl.At(0).LogRecords()
	if recs.Len() != 1 {
		t.Fatalf("expected 1 LogRecord, got %d", recs.Len())
	}
	return recs.At(0)
}

// flattenOurs renders our otel/log record's attributes into a comparable tree.
func flattenOurs(rec log.Record) map[string]any {
	out := map[string]any{}
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		out[kv.Key] = ourValue(kv.Value)
		return true
	})
	return out
}

func ourValue(v log.Value) any {
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
			m[kv.Key] = ourValue(kv.Value)
		}
		return m
	case log.KindSlice:
		s := make([]any, 0)
		for _, e := range v.AsSlice() {
			s = append(s, ourValue(e))
		}
		return s
	default:
		return nil
	}
}

// flattenTheirs renders the SDK's pdata record into the same shape.
func flattenTheirs(rec plog.LogRecord) map[string]any {
	out := map[string]any{}
	rec.Attributes().Range(func(k string, v pcommon.Value) bool {
		out[k] = theirValue(v)
		return true
	})
	return out
}

func theirValue(v pcommon.Value) any {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		return v.Str()
	case pcommon.ValueTypeInt:
		return v.Int()
	case pcommon.ValueTypeDouble:
		return v.Double()
	case pcommon.ValueTypeBool:
		return v.Bool()
	case pcommon.ValueTypeMap:
		m := map[string]any{}
		v.Map().Range(func(k string, vv pcommon.Value) bool {
			m[k] = theirValue(vv)
			return true
		})
		return m
	case pcommon.ValueTypeSlice:
		s := make([]any, 0)
		for i := 0; i < v.Slice().Len(); i++ {
			s = append(s, theirValue(v.Slice().At(i)))
		}
		return s
	default:
		return nil
	}
}

// The shared case: an all-string entity with description, interval and an
// embedded relationship. If our bytes and the SDK's diverge here, one of the
// two has drifted from the frozen contract.
func TestEntityEncodingMatchesToiseSDK(t *testing.T) {
	const iface = "eth0"
	ev := entity.Event{
		Kind:     entity.EntityState,
		Time:     time.Unix(1_700_000_000, 0),
		Interval: 90 * time.Second,
		Entity: &entity.Entity{
			Type:       "network.interface",
			ID:         map[string]any{"host.id": "h-1", "interface.name": iface},
			Attributes: map[string]any{"interface.type": "ethernet"},
			Relationships: []entity.Relationship{{
				Type:       "runs_on",
				TargetType: "host",
				TargetID:   map[string]any{"host.id": "h-1"},
			}},
		},
	}

	_, ours, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	theirs := buildEmitReference(t, "entity.state", emit.Entity{
		Type:       "network.interface",
		ID:         map[string]string{"host.id": "h-1", "interface.name": iface},
		Attributes: map[string]string{"interface.type": "ethernet"},
		Interval:   90 * time.Second,
		Relationships: []emit.Relationship{{
			Type:       "runs_on",
			TargetType: "host",
			TargetID:   map[string]string{"host.id": "h-1"},
		}},
	})

	if got, want := ours.EventName(), theirs.EventName(); got != want {
		t.Errorf("EventName: agent %q, SDK %q", got, want)
	}

	a, b := flattenOurs(ours), flattenTheirs(theirs)
	for _, key := range []string{
		attrEntityType, attrEntityID, attrEntityDescription,
		attrEntityReportInterval, attrEntityRelationships,
	} {
		av, aok := a[key]
		bv, bok := b[key]
		if aok != bok {
			t.Errorf("%s: present in agent=%v, in SDK=%v", key, aok, bok)
			continue
		}
		if !aok {
			continue
		}
		if !equalTree(av, bv) {
			t.Errorf("%s diverges from the Toise SDK:\n  agent: %#v\n  SDK:   %#v", key, av, bv)
		}
	}
}

// The SDK cannot carry what the agent carries: emit.Entity's ID and
// Attributes are map[string]string, ours are map[string]any with scalar
// leaves. Adopting emit as the encoder outright (#455) would stringify every
// numeric attribute — a wire-contract change for the consumer, not a
// consolidation. This test pins the limitation so the day the SDK widens its
// value type, it fails and tells us the swap is unblocked.
func TestToiseSDKCannotCarryNonStringScalars(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1_700_000_000, 0),
		Entity: &entity.Entity{
			Type: "host",
			ID:   map[string]any{"host.id": "h-1"},
			Attributes: map[string]any{
				"host.cpu.frequency.nominal": int64(3_600_000_000),
				"host.virtualization":        true,
			},
		},
	}
	_, ours, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	desc, ok := flattenOurs(ours)[attrEntityDescription].(map[string]any)
	if !ok {
		t.Fatalf("expected a description map, got %#v", flattenOurs(ours)[attrEntityDescription])
	}
	if _, isInt := desc["host.cpu.frequency.nominal"].(int64); !isInt {
		t.Errorf("agent no longer carries int64 attributes; if this is deliberate, "+
			"the #455 swap is unblocked — got %#v", desc["host.cpu.frequency.nominal"])
	}
	if _, isBool := desc["host.virtualization"].(bool); !isBool {
		t.Errorf("agent no longer carries bool attributes; if this is deliberate, "+
			"the #455 swap is unblocked — got %#v", desc["host.virtualization"])
	}
}

func equalTree(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !equalTree(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalTree(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

package otlp

import (
	"testing"
	"time"

	"github.com/toise-dev/toise/pkg/emit"
	"github.com/toise-dev/toise/pkg/emit/conformance"
	"github.com/toise-dev/toise/pkg/emit/wire"
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
	theirs := buildEmitReference(t, wire.EventEntityState, emit.Entity{
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

// Typed scalars survive both encoders identically.
//
// The agent carries descriptive attributes as map[string]any with scalar
// leaves — a nominal CPU frequency is an integer, a virtualization flag a
// boolean. The SDK takes those through Entity.RichAttributes, which lands in
// the same entity.description map on the wire as the string-typed
// Attributes. Both are checked here, because a stringified integer would be
// a silent contract change for the consumer, not a formatting detail.
func TestTypedScalarAttributesMatchToiseSDK(t *testing.T) {
	attrs := map[string]any{
		"host.cpu.frequency.nominal": int64(3_600_000_000),
		"host.virtualization":        true,
		"host.cpu.usage.ratio":       0.25,
		"host.name":                  "shop-preprod",
	}
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1_700_000_000, 0),
		Entity: &entity.Entity{
			Type:       "host",
			ID:         map[string]any{"host.id": "h-1"},
			Attributes: attrs,
		},
	}
	_, ours, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	theirs := buildEmitReference(t, wire.EventEntityState, emit.Entity{
		Type:           "host",
		ID:             map[string]string{"host.id": "h-1"},
		RichAttributes: attrs,
	})

	a := flattenOurs(ours)[attrEntityDescription]
	b := flattenTheirs(theirs)[attrEntityDescription]
	if !equalTree(a, b) {
		t.Errorf("typed descriptive attributes diverge from the Toise SDK:\n"+
			"  agent: %#v\n  SDK:   %#v", a, b)
	}
}

// Identity stays string-formed on both sides, and that is deliberate rather
// than a limitation.
//
// A typed identity value would give one logical identity two spellings — the
// integer 443 and the string "443" — which do not hash alike and would
// produce two entities for one thing. That is precisely the silent
// divergence C6 exists to prevent, so the exact-match equality C6 asserts is
// only applicable while identity has a single canonical form. Descriptive
// attributes carry types; identity carries a form.
func TestIdentityValuesStayStringFormed(t *testing.T) {
	ev := entity.Event{
		Kind: entity.EntityState,
		Time: time.Unix(1_700_000_000, 0),
		Entity: &entity.Entity{
			Type: "service.listener",
			ID:   map[string]any{"host.id": "h-1", "port": "443"},
		},
	}
	_, ours, err := buildEntityRecord(ev)
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}
	id, ok := flattenOurs(ours)[attrEntityID].(map[string]any)
	if !ok {
		t.Fatalf("expected an identity map, got %#v", flattenOurs(ours)[attrEntityID])
	}
	for k, v := range id {
		if _, isStr := v.(string); !isStr {
			t.Errorf("identity key %q carries a non-string value %#v; one logical "+
				"identity would then have two spellings that do not hash alike, "+
				"which is the divergence C6 exists to prevent", k, v)
		}
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

// --- The consumer's conformance kit, run over OUR payload -----------------
//
// Suggested by the consumer after we shipped an alias edge whose belief
// attributes never reached the wire: their kit already flags a same_as with no
// confidence, precisely because such an edge is stored and then ignored by the
// canonical overlay — the cost of sending it and none of the effect.
//
// The differential tests above check that our encoding MATCHES theirs for the
// cases we thought to write. This one checks that whatever we encode SATISFIES
// their contract, including the cases nobody thought to write, and it is the
// check that would have caught the dropped attributes on its own.

// asPdata renders one of our records into the pdata shape the kit consumes.
func asPdata(t *testing.T, eventName string, rec log.Record) plog.Logs {
	t.Helper()
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	// The resource the OTLP strategy actually attaches. Omitting it made the
	// kit report a missing service.instance.id — a fault in the harness, not in
	// the payload: liveness is reference-counted per producer, so the real
	// resource carries it and a bare harness would have reported a defect that
	// production does not have.
	rl.Resource().Attributes().PutStr("service.name", "senhub-agent")
	rl.Resource().Attributes().PutStr("service.instance.id", "3b8d5f21-7c94-4e60-a1d3-6f2b8e05c7a9")
	rl.Resource().Attributes().PutStr("host.id", "8b861704-05bc-4382-b057-7eac3df5e730")
	sl := rl.ScopeLogs().AppendEmpty()
	out := sl.LogRecords().AppendEmpty()
	out.SetEventName(eventName)
	rec.WalkAttributes(func(kv log.KeyValue) bool {
		putValue(out.Attributes().PutEmpty(kv.Key), kv.Value)
		return true
	})
	return ld
}

func putValue(dst pcommon.Value, v log.Value) {
	switch v.Kind() {
	case log.KindString:
		dst.SetStr(v.AsString())
	case log.KindInt64:
		dst.SetInt(v.AsInt64())
	case log.KindFloat64:
		dst.SetDouble(v.AsFloat64())
	case log.KindBool:
		dst.SetBool(v.AsBool())
	case log.KindMap:
		m := dst.SetEmptyMap()
		for _, kv := range v.AsMap() {
			putValue(m.PutEmpty(kv.Key), kv.Value)
		}
	case log.KindSlice:
		s := dst.SetEmptySlice()
		for _, e := range v.AsSlice() {
			putValue(s.AppendEmpty(), e)
		}
	}
}

// A re-key alias must satisfy the kit — which means carrying a usable
// confidence. Without one the edge is inert: stored, then ignored.
func TestConformance_RekeyAliasCarriesAUsableBelief(t *testing.T) {
	_, rec, err := buildEntityRecord(entity.Event{
		Kind: entity.EntityState,
		Entity: &entity.Entity{
			Type: entity.TypeDB,
			ID:   map[string]any{"db.instance.id": "redis:6379@host-1"},
			Attributes: map[string]any{
				"db.system.name": "redis",
			},
			Relationships: []entity.Relationship{{
				Type:       entity.RelSameAs,
				TargetType: entity.TypeDB,
				TargetID:   map[string]any{"db.instance.id": "127.0.0.1:6379"},
				Attributes: map[string]any{"basis": "rekey", "confidence": 1.0},
			}},
		},
	})
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}

	for _, p := range conformance.Check(asPdata(t, wire.EventEntityState, rec)) {
		t.Errorf("conformance: %s", p.String())
	}
}

// The negative: an alias without its belief must be reported, so this test
// fails loudly the day the attributes stop reaching the wire again.
func TestConformance_FlagsAnAliasWithNoBelief(t *testing.T) {
	_, rec, err := buildEntityRecord(entity.Event{
		Kind: entity.EntityState,
		Entity: &entity.Entity{
			Type: entity.TypeDB,
			ID:   map[string]any{"db.instance.id": "redis:6379@host-1"},
			Relationships: []entity.Relationship{{
				Type:       entity.RelSameAs,
				TargetType: entity.TypeDB,
				TargetID:   map[string]any{"db.instance.id": "127.0.0.1:6379"},
				// no basis, no confidence — the pre-fix behaviour
			}},
		},
	})
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}

	problems := conformance.Check(asPdata(t, wire.EventEntityState, rec))
	if len(problems) == 0 {
		t.Fatal("the kit accepted an alias with no confidence; it can no longer guard the regression it exists for")
	}
}

// Every entity type this agent emits must pass the kit, not just the one that
// happened to motivate wiring it in.
func TestConformance_EveryEmittedShapePasses(t *testing.T) {
	cases := []entity.Entity{
		{Type: entity.TypeHost, ID: map[string]any{"host.id": "h-1"},
			Attributes: map[string]any{"host.name": "box", "host.cpu.logical.count": int64(8)}},
		{Type: entity.TypeContainer, ID: map[string]any{"container.id": "9fbff48f8bc0"},
			Attributes: map[string]any{"container.name": "web"}},
		{Type: entity.TypePod, ID: map[string]any{"k8s.pod.uid": "9383348c-ed92"},
			Attributes: map[string]any{"k8s.pod.name": "api-1"},
			Relationships: []entity.Relationship{{
				Type: entity.RelRunsOn, TargetType: entity.TypeHost,
				TargetID: map[string]any{"host.id": "h-1"},
			}}},
		{Type: entity.TypeServiceInstance, ID: map[string]any{"service.instance.id": "swarm://c1"},
			Attributes: map[string]any{"service.name": "docker-swarm", "swarm.node.count": int64(3)}},
	}
	for _, e := range cases {
		ent := e
		t.Run(ent.Type, func(t *testing.T) {
			_, rec, err := buildEntityRecord(entity.Event{Kind: entity.EntityState, Entity: &ent})
			if err != nil {
				t.Fatalf("buildEntityRecord: %v", err)
			}
			for _, p := range conformance.Check(asPdata(t, wire.EventEntityState, rec)) {
				t.Errorf("conformance: %s", p.String())
			}
		})
	}
}

// The consumer's v0.8.0 kit must reject the raw 32-hex machine-id spelling —
// the trap that nearly doubled every Kubernetes node. Verifying their guard
// actually bites is the point of running their kit at all: a check nobody
// tested is a check nobody can rely on.
func TestConformance_RejectsTheRawMachineIDSpelling(t *testing.T) {
	raw := "8b86170405bc4382b0577eac3df5e730"        // /etc/machine-id, verbatim
	dashed := "8b861704-05bc-4382-b057-7eac3df5e730" // what the agent emits

	build := func(id string) []conformance.Problem {
		_, rec, err := buildEntityRecord(entity.Event{
			Kind: entity.EntityState,
			Entity: &entity.Entity{
				Type: entity.TypeHost,
				ID:   map[string]any{"host.id": id},
			},
		})
		if err != nil {
			t.Fatalf("buildEntityRecord: %v", err)
		}
		return conformance.Check(asPdata(t, wire.EventEntityState, rec))
	}

	problems := build(raw)
	if len(problems) == 0 {
		t.Error("the kit accepted the raw machine-id spelling; the guard does not bite")
	}
	for _, p := range problems {
		t.Logf("raw form reported: %s", p.String())
		if p.Advisory {
			t.Error("the host.id spelling is reported as advisory; it was agreed it must fail")
		}
	}

	if got := build(dashed); len(got) != 0 {
		for _, p := range got {
			t.Errorf("the spelling we actually emit was rejected: %s", p.String())
		}
	}
}

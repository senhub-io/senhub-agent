package otlp

import (
	"reflect"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// TestRedactEntityEvent_DropsListedAttributesWithoutMutatingSource pins the
// #682 contract end to end: a host entity.state carrying hw.serial_number +
// cloud.account.id, exported with both keys in redact_attributes, produces a
// record whose attribute payload contains neither key — and the shared
// source entity.Event stays byte-identical, because the event fans out to
// every entity.SubscribeEvents subscriber and must never be mutated.
func TestRedactEntityEvent_DropsListedAttributesWithoutMutatingSource(t *testing.T) {
	src := entity.Event{
		Kind: entity.EntityState,
		Entity: &entity.Entity{
			Type: "host",
			ID:   map[string]any{"host.id": "h-001"},
			Attributes: map[string]any{
				"host.name":        "web-server-1",
				"hw.serial_number": "SN-12345",
				"cloud.account.id": "123456789012",
			},
		},
		Time:     time.Unix(1780272000, 0).UTC(),
		Interval: time.Minute,
	}
	redact := map[string]struct{}{"hw.serial_number": {}, "cloud.account.id": {}}

	_, rec, err := buildEntityRecord(redactEntityEvent(src, redact))
	if err != nil {
		t.Fatalf("buildEntityRecord: %v", err)
	}

	attrs := recordAttrs(rec)
	desc, ok := attrs["entity.description"].(map[string]any)
	if !ok {
		t.Fatalf("entity.description missing or not a map: %#v", attrs)
	}
	for _, k := range []string{"hw.serial_number", "cloud.account.id"} {
		if _, present := desc[k]; present {
			t.Errorf("redacted key %q still present in the record payload", k)
		}
	}
	if desc["host.name"] != "web-server-1" {
		t.Errorf("non-redacted attribute lost: %#v", desc)
	}
	if id, _ := attrs["entity.id"].(map[string]any); id["host.id"] != "h-001" {
		t.Errorf("entity identity must pass through untouched: %#v", attrs["entity.id"])
	}

	wantSource := map[string]any{
		"host.name":        "web-server-1",
		"hw.serial_number": "SN-12345",
		"cloud.account.id": "123456789012",
	}
	if !reflect.DeepEqual(src.Entity.Attributes, wantSource) {
		t.Errorf("source entity.Event.Attributes mutated by redaction: %#v", src.Entity.Attributes)
	}
}

// TestRedactEntityEvent_NoMatchIsPassThrough asserts the filter does not
// copy the entity when nothing matches — the hot path for every deployment
// that leaves redact_attributes empty.
func TestRedactEntityEvent_NoMatchIsPassThrough(t *testing.T) {
	e := &entity.Entity{
		Type:       "host",
		ID:         map[string]any{"host.id": "h-001"},
		Attributes: map[string]any{"host.name": "web-server-1"},
	}
	ev := entity.Event{Kind: entity.EntityState, Entity: e}

	if out := redactEntityEvent(ev, nil); out.Entity != e {
		t.Error("empty redact set must return the event unchanged (no copy)")
	}
	if out := redactEntityEvent(ev, map[string]struct{}{"hw.serial_number": {}}); out.Entity != e {
		t.Error("no matching key must return the event unchanged (no copy)")
	}
}

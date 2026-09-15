package entity

import "testing"

type fixedSource struct {
	obs Observation
	ok  bool
}

func (f *fixedSource) Observe() (Observation, bool) { return f.obs, f.ok }

func TestWithAttributesStampsAbsentKeysAndSparesTheHost(t *testing.T) {
	original := map[string]any{"service.criticality": "low", "db.system": "mysql"}
	src := &fixedSource{ok: true, obs: Observation{Entities: []Entity{
		{Type: "db.instance", ID: map[string]any{"db.instance.id": "a"}, Attributes: original},
		{Type: "host", ID: map[string]any{"host.id": "h"}, Attributes: map[string]any{}},
		{Type: "service.instance", ID: map[string]any{"service.instance.id": "s"}},
	}}}
	gov := map[string]any{"service.criticality": "critical", "entity.label.application": "erp"}

	o, ok := WithAttributes(src, gov).Observe()
	if !ok {
		t.Fatal("observation must pass through")
	}
	db := o.Entities[0].Attributes
	if db["service.criticality"] != "low" {
		t.Errorf("the source's own value must win, got %v", db["service.criticality"])
	}
	if db["entity.label.application"] != "erp" || db["db.system"] != "mysql" {
		t.Errorf("absent keys must be added and the rest kept, got %v", db)
	}
	if _, stamped := original["entity.label.application"]; stamped {
		t.Error("the source's map must not be mutated")
	}
	if len(o.Entities[1].Attributes) != 0 {
		t.Errorf("a host entity must be left alone, got %v", o.Entities[1].Attributes)
	}
	if o.Entities[2].Attributes["entity.label.application"] != "erp" {
		t.Errorf("an entity without attributes must get a fresh map, got %v", o.Entities[2].Attributes)
	}
	if src.obs.Entities[2].Attributes != nil {
		t.Error("the source's entity slice must not be mutated")
	}
}

func TestWithAttributesIsTransparentWhenThereIsNothingToAdd(t *testing.T) {
	src := &fixedSource{ok: false}
	if got := WithAttributes(src, nil); got != Source(src) {
		t.Error("no attributes must return the source itself")
	}
	if got := WithAttributes(nil, map[string]any{"k": "v"}); got != nil {
		t.Error("a nil source must stay nil")
	}
	if _, ok := WithAttributes(src, map[string]any{"k": "v"}).Observe(); ok {
		t.Error("a failed observation must stay failed")
	}
}

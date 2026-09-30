package entity

import (
	"testing"
)

func devKey(id string) map[string]any { return map[string]any{"network.device.id": id} }

// Two sources report the same device in one cycle: a full copy with no
// relation and a thin one carrying the relations. Folding relations onto
// the last copy of a key kept the thin one; the merge keeps one entity
// with every attribute and every relation.
func TestDuplicateCopiesMergeIntoOneFullEntity(t *testing.T) {
	obs := Observation{
		Entities: []Entity{
			{Type: "network.device", ID: devKey("serial:9:A"), Scope: ScopeSNMPIFMIB, Attributes: map[string]any{"sys.name": "core1", "mgmt.ip": "10.0.0.1", "vendor": "cisco"}},
			{Type: "network.interface", ID: map[string]any{"network.device.id": "serial:9:A", "network.interface.name": "eth1"}},
			{Type: "network.device", ID: devKey("serial:9:A"), Scope: ScopeSNMPLLDP, Attributes: map[string]any{"sys.name": "core1"}},
		},
		Relations: []Relation{
			{Type: "has_interface", FromType: "network.device", FromID: devKey("serial:9:A"), ToType: "network.interface", ToID: map[string]any{"network.device.id": "serial:9:A", "network.interface.name": "eth1"}},
			{Type: "has_interface", FromType: "network.device", FromID: devKey("serial:9:A"), ToType: "network.interface", ToID: map[string]any{"network.device.id": "serial:9:A", "network.interface.name": "eth1"}},
		},
	}
	var conflicts []AttributeConflict
	entities, orphans := obs.mergeDuplicates(func(c AttributeConflict) { conflicts = append(conflicts, c) }).foldRelationships()
	if len(orphans) != 0 || len(conflicts) != 0 {
		t.Fatalf("orphans %v, conflicts %v; want none", orphans, conflicts)
	}
	var devs []Entity
	for _, e := range entities {
		if e.Type == "network.device" {
			devs = append(devs, e)
		}
	}
	if len(devs) != 1 {
		t.Fatalf("device entities = %d, want 1", len(devs))
	}
	d := devs[0]
	if len(d.Attributes) != 3 || d.Attributes["mgmt.ip"] != "10.0.0.1" {
		t.Errorf("attributes = %v, want the full set", d.Attributes)
	}
	if len(d.Relationships) != 1 {
		t.Errorf("relationships = %v, want one has_interface (the same relation stated twice is one)", d.Relationships)
	}
}

// Two copies disagree on a value: the kept value is the same whatever the
// order the sources were read in (which changes from one start to the
// next), and the conflict is reported and counted.
func TestAConflictIsResolvedTheSameWayInAnyOrderAndReported(t *testing.T) {
	a := Entity{Type: "network.device", ID: devKey("serial:9:A"), Scope: ScopeSNMPIFMIB, Attributes: map[string]any{"sys.name": "core1"}}
	b := Entity{Type: "network.device", ID: devKey("serial:9:A"), Scope: ScopeSNMPRoute, Attributes: map[string]any{"sys.name": "core-1"}}
	c := Entity{Type: "network.device", ID: devKey("serial:9:A"), Scope: ScopeSNMPIFMIB, Attributes: map[string]any{"sys.name": "CORE1"}}

	var first any
	for _, order := range [][]Entity{{a, b, c}, {c, b, a}, {b, a, c}, {b, c, a}} {
		before := AttributeConflictsTotal()
		var reported int
		out := Observation{Entities: order}.mergeDuplicates(func(AttributeConflict) { reported++ })
		if len(out.Entities) != 1 {
			t.Fatalf("entities = %d, want 1", len(out.Entities))
		}
		got := out.Entities[0].Attributes["sys.name"]
		if first == nil {
			first = got
		} else if got != first {
			t.Fatalf("order %v kept %v, another order kept %v", order, got, first)
		}
		if reported == 0 || AttributeConflictsTotal()-before != uint64(reported) {
			t.Fatalf("reported %d, counted %d; want the conflict reported and counted", reported, AttributeConflictsTotal()-before)
		}
	}
}

package entity

import "testing"

func TestDropOrphanEntities(t *testing.T) {
	hostID := map[string]any{"host.id": "h1"}
	dbID := map[string]any{"db.instance.id": "pg:1"}
	addrID := map[string]any{"network.address": "10.0.0.5"}
	floatID := map[string]any{"network.device.id": "mac:aa"}

	entities := []Entity{
		// host: no relation, must be KEPT (root exception).
		{Type: "host", ID: hostID},
		// db: has an outgoing runs_on -> host, KEPT.
		{Type: "db", ID: dbID, Relationships: []Relationship{
			{Type: "runs_on", TargetType: "host", TargetID: hostID},
		}},
		// network.address: no outgoing edge but referenced by db? no — referenced
		// by the bound_to below from an interface. Add an interface that targets it.
		{Type: "network.interface", ID: map[string]any{"x": "1"}, Relationships: []Relationship{
			{Type: "bound_to", TargetType: "network.address", TargetID: addrID},
		}},
		{Type: "network.address", ID: addrID}, // incoming bound_to -> KEPT
		// floating device: no outgoing, never a target -> DROPPED.
		{Type: "network.device", ID: floatID},
	}

	var dropped []Entity
	kept := dropOrphanEntities(entities, func(d []Entity) { dropped = d })

	keptTypes := map[string]bool{}
	for _, e := range kept {
		keptTypes[e.Type] = true
	}
	if !keptTypes["host"] || !keptTypes["db"] || !keptTypes["network.address"] || !keptTypes["network.interface"] {
		t.Errorf("an anchored/host entity was wrongly dropped; kept=%v", keptTypes)
	}
	if keptTypes["network.device"] {
		t.Errorf("the floating network.device should have been dropped")
	}
	if len(dropped) != 1 || dropped[0].Type != "network.device" {
		t.Errorf("expected exactly the floating device dropped, got %v", dropped)
	}
}

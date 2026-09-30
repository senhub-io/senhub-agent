package entity

import (
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"
)

// AttributeConflict is one attribute two sources of this agent reported
// with different values for the same entity in the same cycle.
type AttributeConflict struct {
	Type      string
	ID        map[string]any
	Attribute string
	Kept      any
	Dropped   any
}

var attributeConflicts atomic.Uint64

// AttributeConflictsTotal is the number of attribute conflicts resolved
// since the start (senhub.agent.entity.attribute.conflicts).
func AttributeConflictsTotal() uint64 { return attributeConflicts.Load() }

// mergeDuplicates collapses the entities of one cycle that share a key into
// one: the union of their attributes, and, once folded, of their relations.
//
// Several sources can report the same entity (an address a route and an
// interface both name, a device polled directly and seen through another's
// LLDP table). Folding relations onto the last copy of a key made the result
// depend on the order the sources were read in, and a thin copy could replace
// a full one.
//
// When copies disagree on a value, the kept one does not depend on that
// order, which changes from one start to the next: the value of the copy
// whose scope sorts first wins, and among copies of the same scope the value
// whose rendering sorts first. The merged entity takes the scope that sorts
// first too, a provisional rule until scopes are readable downstream. Every
// conflict is counted and reported to onConflict: two sources of one agent
// asserting different facts is a defect to fix, not a detail to settle.
func (o Observation) mergeDuplicates(onConflict func(AttributeConflict)) Observation {
	type group struct {
		copies []Entity
	}
	groups := map[string]*group{}
	var order []string
	for _, e := range o.Entities {
		k := entityKey(e.Type, e.ID)
		g, ok := groups[k]
		if !ok {
			g = &group{}
			groups[k] = g
			order = append(order, k)
		}
		g.copies = append(g.copies, e)
	}
	if len(order) == len(o.Entities) {
		return o
	}

	merged := make([]Entity, 0, len(order))
	for _, k := range order {
		copies := groups[k].copies
		if len(copies) == 1 {
			merged = append(merged, copies[0])
			continue
		}
		sort.SliceStable(copies, func(i, j int) bool { return copies[i].Scope < copies[j].Scope })
		out := Entity{Type: copies[0].Type, ID: copies[0].ID, Scope: copies[0].Scope, Attributes: map[string]any{}}
		scopeOf := map[string]string{}
		for _, c := range copies {
			for attr, v := range c.Attributes {
				kept, seen := out.Attributes[attr]
				if !seen {
					out.Attributes[attr] = v
					scopeOf[attr] = c.Scope
					continue
				}
				if reflect.DeepEqual(kept, v) {
					continue
				}
				winner, loser := kept, v
				if c.Scope == scopeOf[attr] && fmt.Sprint(v) < fmt.Sprint(kept) {
					winner, loser = v, kept
				}
				out.Attributes[attr] = winner
				attributeConflicts.Add(1)
				if onConflict != nil {
					onConflict(AttributeConflict{Type: out.Type, ID: out.ID, Attribute: attr, Kept: winner, Dropped: loser})
				}
			}
		}
		merged = append(merged, out)
	}
	o.Entities = merged
	return o
}

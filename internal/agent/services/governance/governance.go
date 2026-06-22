// Package governance turns operator-supplied descriptive metadata into the Toise
// governance attribute vocabulary — entity.owner.*, service.criticality,
// entity.location.*, entity.lifecycle.status, entity.label.* — attachable to any
// entity. Two shapes are supported: a single Governance block (a configured host
// or target) and an ordered Rules set (SNMP discovery — matched against device
// facts). Every attribute is optional; absence never degrades the zero-config
// path (Toise ADR 0030).
package governance

import (
	"fmt"
	"strings"
)

// Governance is operator-supplied descriptive metadata for one entity. The zero
// value contributes no attributes.
type Governance struct {
	OwnerTeam    string
	OwnerContact string
	Criticality  string // critical|high|medium|low
	Site         string
	Datacenter   string
	Rack         string
	Room         string
	Lifecycle    string // active|maintenance|decommissioning|retired (open enum)
	Labels       map[string]string
}

// criticality is a fixed set (Toise pins these exact values); lifecycle is an
// open enum (Toise stores any value but advises the well-known set).
var criticalityValues = map[string]bool{"critical": true, "high": true, "medium": true, "low": true}

// Attributes returns the Toise governance keys for this metadata, omitting every
// empty field. Safe on the zero value (returns an empty map). Operator labels
// are namespaced under the entity.label.* prefix.
func (g Governance) Attributes() map[string]any {
	attrs := map[string]any{}
	set := func(k, v string) {
		if v != "" {
			attrs[k] = v
		}
	}
	set("entity.owner.team", g.OwnerTeam)
	set("entity.owner.contact", g.OwnerContact)
	set("service.criticality", g.Criticality)
	set("entity.location.site", g.Site)
	set("entity.location.datacenter", g.Datacenter)
	set("entity.location.rack", g.Rack)
	set("entity.location.room", g.Room)
	set("entity.lifecycle.status", g.Lifecycle)
	for k, v := range g.Labels {
		if k != "" && v != "" {
			attrs["entity.label."+k] = v
		}
	}
	return attrs
}

// MergeInto applies this metadata's attributes onto dst (this metadata wins on a
// key collision). Used to layer matching discovery rules in order.
func (g Governance) MergeInto(dst map[string]any) {
	for k, v := range g.Attributes() {
		dst[k] = v
	}
}

// IsZero reports whether no field is set.
func (g Governance) IsZero() bool {
	return g.OwnerTeam == "" && g.OwnerContact == "" && g.Criticality == "" &&
		g.Site == "" && g.Datacenter == "" && g.Rack == "" && g.Room == "" &&
		g.Lifecycle == "" && len(g.Labels) == 0
}

// validate rejects an out-of-set criticality (the only closed enum). lifecycle is
// open, so any value is accepted.
func (g Governance) validate() error {
	if g.Criticality != "" && !criticalityValues[g.Criticality] {
		return fmt.Errorf("criticality %q is invalid (want one of critical/high/medium/low)", g.Criticality)
	}
	return nil
}

// Parse builds a Governance from a raw "governance:" mapping (nil → zero value).
// Shape: owner:{team,contact}, criticality, location:{site,datacenter,rack,room},
// lifecycle, labels:{<key>:<value>}.
func Parse(v any) (Governance, error) {
	g := Governance{}
	if v == nil {
		return g, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return g, fmt.Errorf("governance must be a mapping")
	}
	if owner, ok := subMap(m["owner"]); ok {
		g.OwnerTeam = str(owner["team"])
		g.OwnerContact = str(owner["contact"])
	}
	g.Criticality = strings.ToLower(str(m["criticality"]))
	if loc, ok := subMap(m["location"]); ok {
		g.Site = str(loc["site"])
		g.Datacenter = str(loc["datacenter"])
		g.Rack = str(loc["rack"])
		g.Room = str(loc["room"])
	}
	g.Lifecycle = strings.ToLower(str(m["lifecycle"]))
	if labels, ok := subMap(m["labels"]); ok {
		g.Labels = make(map[string]string, len(labels))
		for k, lv := range labels {
			if s := str(lv); s != "" {
				g.Labels[k] = s
			}
		}
	}
	if err := g.validate(); err != nil {
		return Governance{}, err
	}
	return g, nil
}

func subMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

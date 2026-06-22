package governance

import (
	"net"
	"regexp"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("bad test CIDR %q: %v", s, err)
	}
	return n
}

func mustRe(t *testing.T, s string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(s)
	if err != nil {
		t.Fatalf("bad test regex %q: %v", s, err)
	}
	return re
}

func TestAttributes_AllKeysAndLabelPrefix(t *testing.T) {
	g := Governance{
		OwnerTeam: "network-ops", OwnerContact: "noc@ex.com", Criticality: "critical",
		Site: "paris", Datacenter: "dc1", Rack: "R12", Room: "A",
		Lifecycle: "active", Labels: map[string]string{"cost_center": "1234", "empty": ""},
	}
	a := g.Attributes()
	want := map[string]string{
		"entity.owner.team": "network-ops", "entity.owner.contact": "noc@ex.com",
		"service.criticality":  "critical",
		"entity.location.site": "paris", "entity.location.datacenter": "dc1",
		"entity.location.rack": "R12", "entity.location.room": "A",
		"entity.lifecycle.status":  "active",
		"entity.label.cost_center": "1234",
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s = %v, want %v", k, a[k], v)
		}
	}
	if _, present := a["entity.label.empty"]; present {
		t.Error("empty label value must be omitted")
	}
}

func TestAttributes_ZeroValueEmpty(t *testing.T) {
	if a := (Governance{}).Attributes(); len(a) != 0 {
		t.Errorf("zero value must yield no attributes, got %v", a)
	}
	if !(Governance{}).IsZero() {
		t.Error("zero value must be IsZero")
	}
}

func TestParse_NestedAndValidation(t *testing.T) {
	raw := map[string]any{
		"owner":       map[string]any{"team": "db-team", "contact": "dba@ex.com"},
		"criticality": "HIGH", // case-insensitive
		"location":    map[string]any{"site": "lyon", "rack": "R3"},
		"lifecycle":   "Maintenance",
		"labels":      map[string]any{"tier": "gold"},
	}
	g, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if g.OwnerTeam != "db-team" || g.Criticality != "high" || g.Site != "lyon" ||
		g.Rack != "R3" || g.Lifecycle != "maintenance" || g.Labels["tier"] != "gold" {
		t.Errorf("parsed wrong: %+v", g)
	}

	if _, err := Parse(map[string]any{"criticality": "tier_0"}); err == nil {
		t.Error("invalid criticality must be rejected")
	}
	if g, err := Parse(nil); err != nil || !g.IsZero() {
		t.Errorf("nil → zero value, no error: %+v %v", g, err)
	}
}

func TestRules_MatchersAndMerge(t *testing.T) {
	rules := Rules{
		{cidr: mustCIDR(t, "10.0.12.0/24"), gov: Governance{Rack: "R12", OwnerTeam: "net"}},
		{vendor: "cisco", gov: Governance{Labels: map[string]string{"class": "switch"}}},
		{gov: Governance{Site: "paris"}}, // catch-all (no clause)
	}

	// In subnet + cisco + matches the catch-all → all three merge.
	got := rules.Apply(DeviceFacts{IP: "10.0.12.7", Vendor: "Cisco"}) // vendor case-insensitive
	if got["entity.location.rack"] != "R12" || got["entity.owner.team"] != "net" ||
		got["entity.label.class"] != "switch" || got["entity.location.site"] != "paris" {
		t.Errorf("merged attributes wrong: %v", got)
	}

	// Out of subnet, not cisco → only the catch-all applies.
	got = rules.Apply(DeviceFacts{IP: "192.0.2.1", Vendor: "juniper"})
	if got["entity.location.site"] != "paris" {
		t.Errorf("catch-all must still apply: %v", got)
	}
	if _, has := got["entity.location.rack"]; has {
		t.Errorf("out-of-subnet device must not get the rack rule: %v", got)
	}
}

func TestRules_LaterWinsOnCollision(t *testing.T) {
	rules := Rules{
		{gov: Governance{Criticality: "low"}},                                       // broad default
		{cidr: mustCIDR(t, "10.0.0.0/8"), gov: Governance{Criticality: "critical"}}, // specific override
	}
	got := rules.Apply(DeviceFacts{IP: "10.1.2.3"})
	if got["service.criticality"] != "critical" {
		t.Errorf("later matching rule must win: %v", got)
	}
}

func TestRules_SysnameRegexAndAndSemantics(t *testing.T) {
	rules := Rules{
		{cidr: mustCIDR(t, "10.0.0.0/8"), sysName: mustRe(t, "^sw-par-"), gov: Governance{Site: "paris"}},
	}
	// Both clauses match.
	if got := rules.Apply(DeviceFacts{IP: "10.1.1.1", SysName: "sw-par-01"}); got["entity.location.site"] != "paris" {
		t.Errorf("AND match should apply: %v", got)
	}
	// CIDR matches but sysname doesn't → no apply (AND).
	if got := rules.Apply(DeviceFacts{IP: "10.1.1.1", SysName: "rtr-lyon-01"}); len(got) != 0 {
		t.Errorf("AND semantics: sysname mismatch must drop the rule: %v", got)
	}
}

func TestParseRules_Errors(t *testing.T) {
	if _, err := ParseRules([]any{map[string]any{"match": map[string]any{"cidr": "not-a-cidr"}}}); err == nil {
		t.Error("invalid CIDR must error")
	}
	if _, err := ParseRules([]any{map[string]any{"match": map[string]any{"sysname": "([bad"}}}); err == nil {
		t.Error("invalid regex must error")
	}
	if _, err := ParseRules([]any{map[string]any{"governance": map[string]any{"criticality": "bogus"}}}); err == nil {
		t.Error("invalid governance inside a rule must error")
	}
	if rs, err := ParseRules(nil); err != nil || rs != nil {
		t.Errorf("nil → empty, no error: %v %v", rs, err)
	}
}

func TestParseRules_BuildsAndApplies(t *testing.T) {
	raw := []any{
		map[string]any{
			"match":      map[string]any{"cidr": "10.0.12.0/24"},
			"governance": map[string]any{"location": map[string]any{"rack": "R12"}, "criticality": "critical"},
		},
	}
	rs, err := ParseRules(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := rs.Apply(DeviceFacts{IP: "10.0.12.9"})
	if got["entity.location.rack"] != "R12" || got["service.criticality"] != "critical" {
		t.Errorf("parsed rule did not apply: %v", got)
	}
}

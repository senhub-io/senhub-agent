package governance

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// DeviceFacts are the observable facts a discovery rule matches against — the
// values the agent already has at discovery time (poll IP, vendor PEN/name from
// sysObjectID, sysName).
type DeviceFacts struct {
	IP      string
	Vendor  string
	SysName string
}

// Rule is one discovery governance rule: a matcher plus the governance applied
// when it matches. Within a rule, every present match clause must match (AND). A
// rule with no clauses matches every device (a catch-all default).
type Rule struct {
	cidr    *net.IPNet
	vendor  string
	sysName *regexp.Regexp
	gov     Governance
}

func (r Rule) matches(f DeviceFacts) bool {
	if r.cidr != nil {
		ip := net.ParseIP(f.IP)
		if ip == nil || !r.cidr.Contains(ip) {
			return false
		}
	}
	if r.vendor != "" && !strings.EqualFold(r.vendor, f.Vendor) {
		return false
	}
	if r.sysName != nil && !r.sysName.MatchString(f.SysName) {
		return false
	}
	return true
}

// Rules is an ordered governance rule set.
type Rules []Rule

// Apply returns the merged governance attributes of every rule matching f, in
// declaration order (a later matching rule wins on a key collision, so a
// specific rule placed after a broad one overrides it). Empty when none match.
func (rs Rules) Apply(f DeviceFacts) map[string]any {
	attrs := map[string]any{}
	for _, r := range rs {
		if r.matches(f) {
			r.gov.MergeInto(attrs)
		}
	}
	return attrs
}

// ParseRules builds an ordered Rules set from a raw "governance_rules:" list
// (nil → empty). Each item is {match:{cidr,vendor,sysname}, governance:{...}}.
func ParseRules(v any) (Rules, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("governance_rules must be a list")
	}
	rules := make(Rules, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("governance_rules[%d] must be a mapping", i)
		}
		var r Rule
		if mm, ok := subMap(m["match"]); ok {
			if cidr := str(mm["cidr"]); cidr != "" {
				_, ipnet, err := net.ParseCIDR(cidr)
				if err != nil {
					return nil, fmt.Errorf("governance_rules[%d].match.cidr %q: %w", i, cidr, err)
				}
				r.cidr = ipnet
			}
			r.vendor = str(mm["vendor"])
			if sn := str(mm["sysname"]); sn != "" {
				re, err := regexp.Compile(sn)
				if err != nil {
					return nil, fmt.Errorf("governance_rules[%d].match.sysname %q: %w", i, sn, err)
				}
				r.sysName = re
			}
		}
		gov, err := Parse(m["governance"])
		if err != nil {
			return nil, fmt.Errorf("governance_rules[%d].governance: %w", i, err)
		}
		r.gov = gov
		rules = append(rules, r)
	}
	return rules, nil
}

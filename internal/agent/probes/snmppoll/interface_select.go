package snmppoll

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/snmpcore"
)

// Optional ifTable columns read only when an interface selection is
// configured: ifDescr is matched next to ifName, ifAdminStatus feeds
// admin_up_only.
const (
	ifDescr       = "1.3.6.1.2.1.2.2.1.2"
	ifAdminStatus = "1.3.6.1.2.1.2.2.1.7"

	ifAdminUp = 1
)

// ifTypeNumbers names the IANAifType values an operator is likely to filter
// on. A number is always accepted, so a type missing here is still usable.
var ifTypeNumbers = map[string]int{
	"other":            1,
	"ppp":              23,
	"softwareloopback": 24,
	"ethernetcsmacd":   6,
	"iso88023csmacd":   7,
	"fastether":        62,
	"gigabitethernet":  117,
	"ieee80211":        71,
	"propvirtual":      53,
	"tunnel":           131,
	"l2vlan":           135,
	"l3ipvlan":         136,
	"mplstunnel":       150,
	"ieee8023adlag":    161,
}

// ifaceSelect narrows the per-interface metric series. The topology rail
// never consults it: entities keep every interface.
type ifaceSelect struct {
	include       []*regexp.Regexp
	exclude       []*regexp.Regexp
	types         map[int]bool
	adminUpOnly   bool
	operUpOnly    bool
	maxInterfaces int
}

// active reports whether any criterion is set; an inactive selection leaves
// the metric rail exactly as it was.
func (s *ifaceSelect) active() bool {
	return s != nil && (len(s.include) > 0 || len(s.exclude) > 0 || len(s.types) > 0 ||
		s.adminUpOnly || s.operUpOnly || s.maxInterfaces > 0)
}

func parseIfaceSelect(raw interface{}) (*ifaceSelect, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := toStringMap(raw)
	if !ok {
		return nil, fmt.Errorf("'interfaces' must be a mapping")
	}
	sel := &ifaceSelect{}

	var err error
	if sel.include, err = parsePatterns(m["include"], "interfaces.include"); err != nil {
		return nil, err
	}
	if sel.exclude, err = parsePatterns(m["exclude"], "interfaces.exclude"); err != nil {
		return nil, err
	}

	for _, name := range listOfStrings(m["types"]) {
		n, err := parseIfType(name)
		if err != nil {
			return nil, fmt.Errorf("interfaces.types: %w", err)
		}
		if sel.types == nil {
			sel.types = map[int]bool{}
		}
		sel.types[n] = true
	}

	if _, present := m["admin_up_only"]; present {
		b, ok := types.BoolParam(m, "admin_up_only")
		if !ok {
			return nil, fmt.Errorf("interfaces.admin_up_only must be true or false")
		}
		sel.adminUpOnly = b
	}
	if _, present := m["oper_up_only"]; present {
		b, ok := types.BoolParam(m, "oper_up_only")
		if !ok {
			return nil, fmt.Errorf("interfaces.oper_up_only must be true or false")
		}
		sel.operUpOnly = b
	}

	if _, present := m["max_interfaces"]; present {
		n, ok := types.IntParam(m, "max_interfaces")
		if !ok || n < 1 {
			return nil, fmt.Errorf("interfaces.max_interfaces must be a whole number of at least 1")
		}
		sel.maxInterfaces = n
	}
	return sel, nil
}

func parsePatterns(raw interface{}, key string) ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, p := range listOfStrings(raw) {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid regular expression %q: %w", key, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func parseIfType(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, nil
	}
	if n, ok := ifTypeNumbers[s]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("unknown ifType %q (use an IANAifType number or a name such as ethernetCsmacd, ieee8023adLag)", s)
}

func listOfStrings(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch x := item.(type) {
			case string:
				if t := strings.TrimSpace(x); t != "" {
					out = append(out, t)
				}
			case int:
				out = append(out, strconv.Itoa(x))
			}
		}
		return out
	case string:
		if t := strings.TrimSpace(v); t != "" {
			return []string{t}
		}
	}
	return nil
}

// matches applies every criterion except the cap. Regular expressions are
// tried against ifName and ifDescr: vendors disagree on which of the two
// carries the readable port name.
func (s *ifaceSelect) matches(r ifaceRow) bool {
	if len(s.include) > 0 && !matchesAny(s.include, r.Name, r.Descr) {
		return false
	}
	if matchesAny(s.exclude, r.Name, r.Descr) {
		return false
	}
	if len(s.types) > 0 && !s.types[r.IfType] {
		return false
	}
	if s.adminUpOnly && r.AdminStatus != ifAdminUp {
		return false
	}
	if s.operUpOnly && r.OperStatus != ifOperUp {
		return false
	}
	return true
}

func matchesAny(res []*regexp.Regexp, texts ...string) bool {
	for _, re := range res {
		for _, t := range texts {
			if t != "" && re.MatchString(t) {
				return true
			}
		}
	}
	return false
}

// selectIndexes returns the ifIndex set that keeps its metric series, and
// how many interfaces matched before max_interfaces cut the list. The cap
// keeps the lowest ifIndex values: they are stable across reboots, so the
// same ports survive from one sweep to the next.
func (s *ifaceSelect) selectIndexes(rows []ifaceRow) (map[string]bool, int) {
	matched := make([]string, 0, len(rows))
	for _, r := range rows {
		if s.matches(r) {
			matched = append(matched, r.Index)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		a, errA := strconv.Atoi(matched[i])
		b, errB := strconv.Atoi(matched[j])
		if errA != nil || errB != nil {
			return matched[i] < matched[j]
		}
		return a < b
	})
	total := len(matched)
	if s.maxInterfaces > 0 && total > s.maxInterfaces {
		matched = matched[:s.maxInterfaces]
	}
	out := make(map[string]bool, len(matched))
	for _, idx := range matched {
		out[idx] = true
	}
	return out, total
}

// collectSelectionFields fills the columns the selection needs beyond the
// inventory walk. Both are best-effort: a column that cannot be read leaves
// its criterion failing closed (a port of unknown state is not "up"), and
// the names of the walks that failed are returned for the caller to log.
func collectSelectionFields(client snmpClient, rows []ifaceRow, sel *ifaceSelect) []string {
	var skipped []string
	byIndex := make(map[string]*ifaceRow, len(rows))
	for i := range rows {
		byIndex[rows[i].Index] = &rows[i]
	}
	if len(sel.include) > 0 || len(sel.exclude) > 0 {
		binds, err := client.WalkRaw(ifDescr)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("ifDescr: %v", err))
		}
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, ifDescr+"."); ok {
				if r := byIndex[idx]; r != nil {
					r.Descr = snmpcore.OctetText(snmpcore.AsBytes(b.Value))
				}
			}
		}
	}
	if sel.adminUpOnly {
		binds, err := client.WalkRaw(ifAdminStatus)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("ifAdminStatus: %v", err))
		}
		for _, b := range binds {
			if idx, ok := strings.CutPrefix(b.OID, ifAdminStatus+"."); ok {
				if r := byIndex[idx]; r != nil {
					if v, ok := snmpcore.AsInt(b.Value); ok {
						r.AdminStatus = v
					}
				}
			}
		}
	}
	return skipped
}

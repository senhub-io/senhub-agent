package template

import (
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// probeMacroName carries the probe instance name, discovered like any
// other dimension so one template serves every instance of a type.
const probeMacroName = "{#PROBE}"

// A variant family is the set of metrics of one probe that share an OTel
// name and a dimension set and differ only by the values of the same
// attributes: the used and the free bytes of a filesystem, the bytes
// sent and received on an interface, the modes of a processor.
//
// Declaring one prototype per member is what leaves a third of a host's
// items empty forever, because a platform feeds only part of a family: a
// Linux filesystem reports what it uses and not what a Windows drive
// reports free, and the generator cannot know which from the definition
// alone. The agent can, because discovery runs on the host over the
// series actually collected.
//
// So a family gets one prototype whose key carries the attribute as a
// macro, hanging under its own discovery rule, and the agent returns
// only the values it feeds. The empty items disappear without the
// generator having to guess, and there are fewer prototypes than before.
type family struct {
	otelName string
	labels   []string
	attrKeys []string
	members  []transformers.MetricDefinition
}

// variantFamilies returns the families of a definition keyed by their
// own identity, plus the family each qualifying metric belongs to.
func variantFamilies(def transformers.ProbeDefinition) (map[string]*family, map[string]*family) {
	grouped := map[string]*family{}
	var order []string
	for _, m := range def.Metrics {
		if m.Otel != nil && m.Otel.Skip {
			continue
		}
		if len(attributeKeys(m)) == 0 {
			// Without an attribute there is nothing to discover: the
			// metric is alone in its key and stays where it was.
			continue
		}
		labels := dimensions(def, m)
		id := otelNameOf(m) + "\x00" + strings.Join(labels, ",")
		f, ok := grouped[id]
		if !ok {
			f = &family{otelName: otelNameOf(m), labels: labels}
			grouped[id] = f
			order = append(order, id)
		}
		f.members = append(f.members, m)
	}

	families := map[string]*family{}
	byMetric := map[string]*family{}
	for _, id := range order {
		f := grouped[id]
		if len(f.members) < 2 {
			continue
		}
		// Every member must be told apart by the same attributes. When
		// one is not, the group mixes an aggregate with its parts and
		// collapsing them would invent a key nobody sends.
		keys := attributeKeys(f.members[0])
		sameShape := true
		values := map[string]bool{}
		for _, m := range f.members {
			if !equalStrings(attributeKeys(m), keys) {
				sameShape = false
				break
			}
			values[strings.Join(attributeValues(m), "\x00")] = true
		}
		// Two members can carry the same attribute value; they already
		// resolve to one key and the generator already declares it once.
		// What matters is that the family really has something to
		// discover, so at least two values must differ.
		if !sameShape || len(values) < 2 {
			continue
		}
		f.attrKeys = keys
		families[id] = f
		for _, m := range f.members {
			byMetric[m.Name] = f
		}
	}
	return families, byMetric
}

func otelNameOf(m transformers.MetricDefinition) string {
	if m.Otel != nil && m.Otel.Name != "" {
		return m.Otel.Name
	}
	return m.Name
}

func attributeKeys(m transformers.MetricDefinition) []string {
	if m.Otel == nil || len(m.Otel.Attributes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Otel.Attributes))
	for k := range m.Otel.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func attributeValues(m transformers.MetricDefinition) []string {
	keys := attributeKeys(m)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = m.Otel.Attributes[k]
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// attrMacros names the macro carrying each attribute. The last segment
// of the attribute key reads best on a host page, so cpu.mode becomes
// {#MODE}; the whole key is used when the short form would collide with
// a dimension's macro or with another attribute of the same family.
func attrMacros(labels, attrKeys []string) []string {
	taken := map[string]bool{probeMacroName: true}
	for _, l := range labels {
		taken[macroFor(l)] = true
	}
	short := make([]string, len(attrKeys))
	seen := map[string]int{}
	for i, k := range attrKeys {
		s := macroFor(lastSegment(k))
		short[i] = s
		seen[s]++
	}
	out := make([]string, len(attrKeys))
	for i, k := range attrKeys {
		if taken[short[i]] || seen[short[i]] > 1 {
			out[i] = macroFor(k)
			continue
		}
		out[i] = short[i]
	}
	return out
}

func lastSegment(k string) string {
	if i := strings.LastIndex(k, "."); i >= 0 && i+1 < len(k) {
		return k[i+1:]
	}
	return k
}

// variantRuleKey names the discovery rule of one family. It is a key of
// its own rather than a parameter of the probe-level rule, because the
// rows differ: this one enumerates the attribute values actually fed.
func variantRuleKey(prefix, probeType, otelName string, labels []string) string {
	return buildKey(prefix, "discovery.variants", append([]string{probeType, otelName}, labels...))
}

// variantPrototypeKey is the family's single key, with the attribute
// values replaced by the macros the rule discovers.
func variantPrototypeKey(prefix string, f *family) string {
	params := []string{probeMacroName}
	for _, l := range f.labels {
		params = append(params, macroFor(l))
	}
	params = append(params, attrMacros(f.labels, f.attrKeys)...)
	return buildKey(prefix, f.otelName, params)
}

// variantPrototypeName builds one name for the whole family out of what
// the curated display names have in common, before and after the part
// that varies: "Network {interface} Send Errors" and "... Receive
// Errors" keep both ends and become "Network {#INTERFACE} Errors
// ({#DIRECTION})". Members that share no wording fall back to the OTel
// name. The dimensions are named too when the stem does not carry them,
// because otherwise every instance under the rule shows the same line.
func variantPrototypeName(f *family) string {
	displays := make([]string, 0, len(f.members))
	for _, m := range f.members {
		displays = append(displays, firstNonEmpty(m.DisplayName, m.Name))
	}
	head := trimToWord(commonPrefix(displays), false)
	tail := trimToWord(commonSuffix(displays), true)
	stem := strings.TrimSpace(head + " " + tail)
	if len([]rune(stem)) < 3 {
		stem = humanise(f.otelName)
	}
	stem = tagPlaceholder.ReplaceAllStringFunc(stem, func(ph string) string {
		return macroFor(strings.Trim(ph, "{}"))
	})
	// A curated name often ends with its own parenthetical naming the
	// instance. Take it back apart rather than adding a second one.
	stem, named := splitTrailingParenthetical(stem)
	var parts []string
	for _, l := range f.labels {
		macro := macroFor(l)
		if strings.Contains(stem, macro) {
			continue
		}
		if named != "" && !strings.Contains(named, macro) {
			continue
		}
		parts = append(parts, macro)
	}
	parts = append(parts, attrMacros(f.labels, f.attrKeys)...)
	return probeMacroName + ": " + stem + " (" + strings.Join(parts, ", ") + ")"
}

func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for p != "" && !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

func commonSuffix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for p != "" && !strings.HasSuffix(s, p) {
			p = p[1:]
		}
	}
	return p
}

// trimToWord cuts a common affix back to a word boundary. A prefix that
// already ends on one is kept whole, so "Network Bytes " survives; one
// that stops mid-word is cut back, so "Memory Us" becomes "Memory".
func trimToWord(s string, suffix bool) string {
	if s == "" {
		return ""
	}
	if suffix {
		if strings.HasPrefix(s, " ") {
			return strings.TrimLeft(s, " ")
		}
		if i := strings.Index(s, " "); i >= 0 {
			return strings.TrimLeft(s[i:], " ")
		}
		return ""
	}
	if strings.HasSuffix(s, " ") || strings.HasSuffix(s, "(") {
		return strings.TrimRight(s, " (")
	}
	if i := strings.LastIndexAny(s, " ("); i >= 0 {
		return strings.TrimRight(s[:i], " (")
	}
	return ""
}

// humanise turns an OTel metric name into a display name, dropping the
// namespace: senhub.system.disk.io becomes "Disk Io".
func humanise(otelName string) string {
	parts := strings.Split(otelName, ".")
	for len(parts) > 2 {
		parts = parts[1:]
	}
	for i, p := range parts {
		p = strings.ReplaceAll(p, "_", " ")
		if p != "" {
			p = strings.ToUpper(p[:1]) + p[1:]
		}
		parts[i] = p
	}
	return strings.Join(parts, " ")
}

// variantRuleName titles a family's rule with what it enumerates, so an
// operator reading the discovery list of a host sees the difference
// between the rule that finds the instances and the one that finds which
// values of a metric this host feeds.
func variantRuleName(probeType string, f *family) string {
	title := "SenHub " + probeType + " " + humanise(f.otelName) + " by " +
		strings.Join(trimmedAttrNames(f), ", ")
	if len(f.labels) > 0 {
		title += " and " + strings.Join(f.labels, ", ")
	}
	return title
}

func trimmedAttrNames(f *family) []string {
	out := make([]string, len(f.attrKeys))
	for i, k := range f.attrKeys {
		out[i] = lastSegment(k)
	}
	return out
}

// splitTrailingParenthetical takes "Inodes ({#MOUNT_POINT})" apart into
// its wording and what it named, so one parenthetical can be rebuilt
// with the family's own macro added.
func splitTrailingParenthetical(s string) (string, string) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, ")") {
		return s, ""
	}
	i := strings.LastIndex(s, "(")
	if i < 0 {
		return s, ""
	}
	return strings.TrimSpace(s[:i]), s[i+1 : len(s)-1]
}

// forPlatform drops the metrics the named platform cannot produce. It
// runs before the families are formed, so a family whose other members
// are Windows-only dissolves on Linux and its survivor keeps its own
// key, which is what the agent sends there.
func forPlatform(def transformers.ProbeDefinition, goos string) transformers.ProbeDefinition {
	if goos == "" {
		return def
	}
	kept := make([]transformers.MetricDefinition, 0, len(def.Metrics))
	for _, m := range def.Metrics {
		if m.RunsOn(goos) {
			kept = append(kept, m)
		}
	}
	def.Metrics = kept
	return def
}

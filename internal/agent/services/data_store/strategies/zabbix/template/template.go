// Package template generates Zabbix templates from the probe definitions,
// so that the items the server asks for are exactly the keys the zabbix
// output sends: both come from the same YAML.
//
// One template is written per probe type. Every item is a prototype
// under a discovery rule, because the probe instance name is itself
// discovered ({#PROBE}); a metric with dimensions hangs under the rule
// of its dimension set, with one macro per dimension.
package template

import (
	"crypto/md5" // #nosec G501 - identifier derivation, not a security use
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// LookupSource resolves a lookup id (the `lookup:` of a metric) into the
// code → text mapping that becomes a Zabbix value map.
type LookupSource interface {
	Lookup(id string) (map[int]string, bool)
}

// Options tunes a generation.
type Options struct {
	// Prefix is the first segment of every key; must match the output's key_prefix.
	Prefix string
	// Version is the export version written in the file: "6.0" or "7.0".
	Version string
	// ItemDelay is the update interval of the prototypes; DiscoveryDelay that of the rules.
	ItemDelay, DiscoveryDelay string
	// Group is the template group the template is filed under.
	Group string
	// Lookups provides the value maps; nil means none.
	Lookups LookupSource
}

func (o Options) withDefaults() Options {
	if o.Prefix == "" {
		o.Prefix = "senhub"
	}
	if o.Version == "" {
		o.Version = "7.0"
	}
	if o.ItemDelay == "" {
		o.ItemDelay = "1m"
	}
	if o.DiscoveryDelay == "" {
		o.DiscoveryDelay = "1h"
	}
	if o.Group == "" {
		o.Group = "Templates"
	}
	return o
}

// The export document, shaped like `configuration.export` output.

type Export struct {
	ZabbixExport ExportBody `yaml:"zabbix_export"`
}

type ExportBody struct {
	Version        string          `yaml:"version"`
	TemplateGroups []TemplateGroup `yaml:"template_groups"`
	Templates      []Template      `yaml:"templates"`
}

type TemplateGroup struct {
	UUID string `yaml:"uuid"`
	Name string `yaml:"name"`
}

type Template struct {
	UUID           string          `yaml:"uuid"`
	Template       string          `yaml:"template"`
	Name           string          `yaml:"name"`
	Description    string          `yaml:"description,omitempty"`
	Groups         []GroupRef      `yaml:"groups"`
	DiscoveryRules []DiscoveryRule `yaml:"discovery_rules,omitempty"`
	ValueMaps      []ValueMap      `yaml:"valuemaps,omitempty"`
}

type GroupRef struct {
	Name string `yaml:"name"`
}

type DiscoveryRule struct {
	UUID           string          `yaml:"uuid"`
	Name           string          `yaml:"name"`
	Type           string          `yaml:"type"`
	Key            string          `yaml:"key"`
	Delay          string          `yaml:"delay"`
	Description    string          `yaml:"description,omitempty"`
	ItemPrototypes []ItemPrototype `yaml:"item_prototypes"`
}

type ItemPrototype struct {
	UUID        string    `yaml:"uuid"`
	Name        string    `yaml:"name"`
	Type        string    `yaml:"type"`
	Key         string    `yaml:"key"`
	Delay       string    `yaml:"delay"`
	ValueType   string    `yaml:"value_type"`
	Units       string    `yaml:"units,omitempty"`
	Description string    `yaml:"description,omitempty"`
	ValueMap    *ValueRef `yaml:"valuemap,omitempty"`
}

type ValueRef struct {
	Name string `yaml:"name"`
}

type ValueMap struct {
	UUID     string    `yaml:"uuid"`
	Name     string    `yaml:"name"`
	Mappings []Mapping `yaml:"mappings"`
}

type Mapping struct {
	Value    string `yaml:"value"`
	NewValue string `yaml:"newvalue"`
}

// Generate builds the template of one probe type.
func Generate(def transformers.ProbeDefinition, opts Options) (Export, error) {
	opts = opts.withDefaults()
	if def.ProbeName == "" {
		return Export{}, fmt.Errorf("definition without a probe_name")
	}
	name := "SenHub " + firstNonEmpty(def.FriendlyName, def.ProbeName)
	tpl := Template{
		UUID:        uid("template", name),
		Template:    name,
		Name:        name,
		Description: fmt.Sprintf("Generated from the SenHub Agent definition of the %s probe; the agent's zabbix output sends these keys.", def.ProbeName),
		Groups:      []GroupRef{{Name: opts.Group}},
	}

	rules := map[string]*DiscoveryRule{}
	var order []string
	valueMaps := map[string]ValueMap{}
	seenKeys := map[string]bool{}

	for _, m := range def.Metrics {
		if m.Otel != nil && m.Otel.Skip {
			continue
		}
		labels := dimensions(def, m)
		// Two internal metrics can resolve to one key (same OTel name,
		// same attributes, same dimensions); the agent sends that key
		// once, so the template declares it once.
		key := prototypeKey(opts.Prefix, m, labels)
		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true
		ruleKey := discoveryKey(opts.Prefix, def.ProbeName, labels)
		rule, ok := rules[ruleKey]
		if !ok {
			rule = &DiscoveryRule{
				UUID:        uid("rule", name, ruleKey),
				Name:        ruleName(def.ProbeName, labels),
				Type:        "ZABBIX_ACTIVE",
				Key:         ruleKey,
				Delay:       opts.DiscoveryDelay,
				Description: "Served by the SenHub Agent zabbix output.",
			}
			rules[ruleKey] = rule
			order = append(order, ruleKey)
		}
		proto := ItemPrototype{
			Name:        prototypeName(m, labels),
			Type:        "ZABBIX_ACTIVE",
			Key:         prototypeKey(opts.Prefix, m, labels),
			Delay:       opts.ItemDelay,
			ValueType:   "FLOAT",
			Units:       units(m),
			Description: m.Description,
		}
		proto.UUID = uid("item", name, proto.Key)
		if m.Lookup != "" && opts.Lookups != nil {
			if mapping, ok := opts.Lookups.Lookup(m.Lookup); ok && len(mapping) > 0 {
				if _, seen := valueMaps[m.Lookup]; !seen {
					valueMaps[m.Lookup] = valueMap(name, m.Lookup, mapping)
				}
				proto.ValueMap = &ValueRef{Name: m.Lookup}
			}
		}
		rule.ItemPrototypes = append(rule.ItemPrototypes, proto)
	}

	for _, k := range order {
		tpl.DiscoveryRules = append(tpl.DiscoveryRules, *rules[k])
	}
	vmNames := make([]string, 0, len(valueMaps))
	for n := range valueMaps {
		vmNames = append(vmNames, n)
	}
	sort.Strings(vmNames)
	for _, n := range vmNames {
		tpl.ValueMaps = append(tpl.ValueMaps, valueMaps[n])
	}

	return Export{ZabbixExport: ExportBody{
		Version:        opts.Version,
		TemplateGroups: []TemplateGroup{{UUID: uid("group", opts.Group), Name: opts.Group}},
		Templates:      []Template{tpl},
	}}, nil
}

// Encode writes the export as the YAML Zabbix imports.
func Encode(e Export) ([]byte, error) {
	return yaml.Marshal(e)
}

// The key builders below mirror the zabbix output's keys.go: same
// sanitising, same parameter order, so a generated prototype with its
// macros substituted is the key the agent sends.

func sanitizeKeyName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func buildKey(prefix, name string, params []string) string {
	key := sanitizeKeyName(name)
	if p := sanitizeKeyName(prefix); p != "" && !strings.HasPrefix(key, p+".") {
		key = p + "." + key
	}
	if len(params) == 0 {
		return key
	}
	return key + "[" + strings.Join(params, ",") + "]"
}

func discoveryKey(prefix, probeType string, labels []string) string {
	return buildKey(prefix, "discovery", append([]string{probeType}, labels...))
}

func macroFor(label string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(label) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return "{#" + b.String() + "}"
}

func dimensions(def transformers.ProbeDefinition, m transformers.MetricDefinition) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range append(append([]string{}, def.MultiInstanceLabels...), m.MultiInstanceLabels...) {
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

func prototypeKey(prefix string, m transformers.MetricDefinition, labels []string) string {
	name := m.Name
	if m.Otel != nil && m.Otel.Name != "" {
		name = m.Otel.Name
	}
	params := []string{"{#PROBE}"}
	for _, l := range labels {
		params = append(params, macroFor(l))
	}
	if m.Otel != nil && len(m.Otel.Attributes) > 0 {
		keys := make([]string, 0, len(m.Otel.Attributes))
		for k := range m.Otel.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			params = append(params, m.Otel.Attributes[k])
		}
	}
	return buildKey(prefix, name, params)
}

var tagPlaceholder = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_.]*)\}`)

// prototypeName is the operator-facing name: the display name with its
// {tag} placeholders turned into macros, the probe first.
func prototypeName(m transformers.MetricDefinition, labels []string) string {
	display := firstNonEmpty(m.DisplayName, m.Name)
	display = tagPlaceholder.ReplaceAllStringFunc(display, func(ph string) string {
		return macroFor(strings.Trim(ph, "{}"))
	})
	if m.Otel != nil && len(m.Otel.Attributes) > 0 && !strings.Contains(display, "{#") {
		// Metrics collapsed onto one OTel name usually carry the
		// discriminating attribute in their display name already
		// ("CPU User"); when they do not, add it so the prototypes
		// under one rule keep distinct names.
		keys := make([]string, 0, len(m.Otel.Attributes))
		for k := range m.Otel.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, m.Otel.Attributes[k])
		}
		if !strings.Contains(strings.ToLower(display), strings.ToLower(parts[0])) {
			display += " (" + strings.Join(parts, ", ") + ")"
		}
	}
	return "{#PROBE}: " + display
}

func ruleName(probeType string, labels []string) string {
	if len(labels) == 0 {
		return "SenHub " + probeType + " instances"
	}
	return "SenHub " + probeType + " by " + strings.Join(labels, ", ")
}

// units maps the OTel unit to what Zabbix displays; Zabbix applies its
// own multipliers to B and bps, and shows the rest verbatim.
func units(m transformers.MetricDefinition) string {
	if m.Otel == nil || m.Otel.Name == "" {
		return m.Unit
	}
	switch m.Otel.Unit {
	case "By":
		return "B"
	case "By/s":
		return "Bps"
	case "bit/s":
		return "bps"
	case "1", "", "{count}":
		return ""
	default:
		if strings.HasPrefix(m.Otel.Unit, "{") {
			return ""
		}
		return m.Otel.Unit
	}
}

func valueMap(template, id string, mapping map[int]string) ValueMap {
	codes := make([]int, 0, len(mapping))
	for c := range mapping {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	vm := ValueMap{UUID: uid("valuemap", template, id), Name: id}
	for _, c := range codes {
		vm.Mappings = append(vm.Mappings, Mapping{Value: strconv.Itoa(c), NewValue: mapping[c]})
	}
	return vm
}

// uid derives a stable identifier from what the object is, so a
// re-export updates the same objects instead of duplicating them. Zabbix
// checks the shape of a UUID version 4, so the version and variant
// nibbles are forced onto the hash.
func uid(parts ...string) string {
	sum := md5.Sum([]byte(strings.Join(parts, "\x00"))) // #nosec G401 - identifier derivation
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

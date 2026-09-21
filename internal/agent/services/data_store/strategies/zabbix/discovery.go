package zabbix

import (
	"encoding/json"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Low-level discovery lets the server create the items of a series whose
// identity is only known at collection time: one item per filesystem,
// per interface, per database. The agent serves one discovery key per
// probe type and per set of dimensions:
//
//	<prefix>.discovery[<probe type>,<label>,...]
//
// whose value is the JSON array Zabbix expects, one object per instance,
// with the probe name under {#PROBE} and every dimension under its own
// macro ({#DEVICE}, {#MOUNT_POINT}). A template generated from the
// definitions declares the matching rules and prototypes, with the same
// macros in the prototype keys, so the probe name itself is discovered
// and a template is written per probe type rather than per instance.

const probeMacro = "{#PROBE}"

// macroFor turns a tag key into the Zabbix macro that carries its value.
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

// discoveryKey names the discovery rule of one probe type and one set
// of dimensions; no dimension gives the rule that discovers the probe
// instances alone.
func discoveryKey(prefix, probeType string, labels []string) string {
	params := append([]string{probeType}, labels...)
	return buildKey(prefix, "discovery", params)
}

// discoveryItems renders the discovery values for the series currently
// held, one item per (probe type, dimension set) seen.
func discoveryItems(prefix string, defs otelmapper.DefinitionLookup, metrics []otelmapper.CacheMetric) []item {
	type rule struct {
		probeType string
		labels    []string
		instances map[string]map[string]string
	}
	rules := map[string]*rule{}
	for _, cm := range metrics {
		var def *transformers.ProbeDefinition
		if defs != nil {
			def = defs.GetProbeDefinition(cm.ProbeType)
		}
		labels := dimensions(def, findMetric(def, cm.MetricName))
		key := discoveryKey(prefix, cm.ProbeType, labels)
		r, ok := rules[key]
		if !ok {
			r = &rule{probeType: cm.ProbeType, labels: labels, instances: map[string]map[string]string{}}
			rules[key] = r
		}
		// A series that carries none of the rule's labels is not an
		// instance: it is the probe's own aggregate, published beside
		// the per-instance ones. Discovering it creates an item whose
		// name ends in empty parentheses, which an operator reads as a
		// defect rather than as a total.
		blank := false
		for _, l := range labels {
			if cm.Tags[l] == "" {
				blank = true
				break
			}
		}
		if blank && len(labels) > 0 {
			continue
		}
		entry := map[string]string{probeMacro: cm.ProbeName}
		id := cm.ProbeName
		for _, l := range labels {
			v := cm.Tags[l]
			entry[macroFor(l)] = v
			id += "\x00" + v
		}
		r.instances[id] = entry
	}

	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]item, 0, len(keys))
	for _, k := range keys {
		r := rules[k]
		ids := make([]string, 0, len(r.instances))
		for id := range r.instances {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, r.instances[id])
		}
		body, err := json.Marshal(rows)
		if err != nil {
			continue
		}
		out = append(out, item{Key: k, Value: string(body)})
	}
	return out
}

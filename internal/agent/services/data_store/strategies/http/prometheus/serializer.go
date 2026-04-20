package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strconv"
)

// SerializeOptions tweaks the output format (reserved for future flags,
// e.g. timestamps, OpenMetrics vs classic, custom prefix).
type SerializeOptions struct {
	// IncludeTimestamp, when true, appends the record timestamp (unix ms)
	// to each metric line. Most Prometheus users want this OFF (the scraper
	// records scrape time). OFF by default.
	IncludeTimestamp bool
}

// SerializeToTextExposition writes the Prometheus text exposition format v0.0.4.
// Records are grouped by their resolved Prometheus name and emitted with a
// single `# HELP` and `# TYPE` header per group, followed by all label
// variants of the metric.
//
// Reference: https://prometheus.io/docs/instrumenting/exposition_formats/
func SerializeToTextExposition(records []OtelRecord, w io.Writer, opts SerializeOptions) error {
	// Group records by their final Prometheus metric name. Two OTel metrics
	// collapsed into the same name (common pattern — e.g. hw.status emitted
	// from drive.health AND drive.failure_predicted) are merged here.
	type group struct {
		promName string
		promType string
		help     string
		unit     string
		rows     []OtelRecord
	}
	groups := map[string]*group{}
	// Preserve first-seen order for stable, human-friendly output.
	var order []string

	for _, r := range records {
		promName := OTelNameToPromName(r.Name, r.Unit, r.Type)
		g, ok := groups[promName]
		if !ok {
			g = &group{
				promName: promName,
				promType: PromType(r.Type),
				help:     r.Description,
				unit:     r.Unit,
			}
			groups[promName] = g
			order = append(order, promName)
		} else if g.help == "" && r.Description != "" {
			// Fill help from later records if the first one had no description.
			g.help = r.Description
		}
		g.rows = append(g.rows, r)
	}

	for _, promName := range order {
		g := groups[promName]
		if err := writeGroup(w, g.promName, g.promType, g.help, g.rows, opts); err != nil {
			return err
		}
	}
	return nil
}

func writeGroup(w io.Writer, name, promType, help string, rows []OtelRecord, opts SerializeOptions) error {
	if help != "" {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, HelpString(help)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, promType); err != nil {
		return err
	}

	// Sort rows deterministically for stable output (makes scraper diffs
	// and test assertions easier).
	sort.Slice(rows, func(i, j int) bool {
		return labelString(rows[i].Attributes) < labelString(rows[j].Attributes)
	})

	for _, r := range rows {
		labelKeys := make([]string, 0, len(r.Attributes))
		promLabels := make(map[string]string, len(r.Attributes))
		for k, v := range r.Attributes {
			pk := OTelAttributeToPromLabel(k)
			labelKeys = append(labelKeys, pk)
			promLabels[pk] = v
		}
		sort.Strings(labelKeys)
		labels := FormatLabels(labelKeys, promLabels)
		valueStr := formatValue(r.Value)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", name, labels, valueStr); err != nil {
			return err
		}
	}
	// Blank line between groups for readability (not required by the spec).
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}

// labelString produces a stable string representation of a label set
// used for sorting rows within a group.
func labelString(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf []byte
	for _, k := range keys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, attrs[k]...)
		buf = append(buf, ',')
	}
	return string(buf)
}

// formatValue formats a float64 per the Prometheus exposition format.
// Special values: +Inf → "+Inf", -Inf → "-Inf", NaN → "NaN".
func formatValue(v float64) string {
	// Integer-valued floats render without decimal for readability.
	if v == float64(int64(v)) && v > -1e15 && v < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

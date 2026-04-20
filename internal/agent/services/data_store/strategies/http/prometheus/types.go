package prometheus

// OtelRecord is a resolved, OTel-shaped data point ready for Prometheus
// serialization. One CachedMetric can produce multiple OtelRecords via the
// `expand:` directive (one per hw.state value, for instance).
//
// Values here are AFTER unit conversions (% → ratio, MB → By, etc.) —
// the serializer writes them as-is into the text exposition output.
type OtelRecord struct {
	// OTel name (with dots, before Prometheus transformation).
	// Example: "system.cpu.time", "senhub.netscaler.lbvserver.status"
	Name string

	// OTel unit (UCUM). Example: "s", "By", "1", "{packet}", "bit/s"
	Unit string

	// OTel metric type: "counter", "gauge", "updowncounter", "histogram"
	Type string

	// Attributes merged in order: (1) static attributes from the YAML
	// `otel.attributes` block, (2) tag_to_attribute mappings from probe tags,
	// (3) the expand-produced attribute (e.g. hw.state=ok), (4) systematic
	// labels (probe_name, probe_type, custom_tags).
	// Attribute names keep their OTel dotted form; they get transformed at
	// Prometheus serialization time by OTelAttributeToPromLabel.
	Attributes map[string]string

	// Value after unit/scale conversion. Ready to write.
	Value float64

	// Description copied from the YAML (used for the Prometheus `# HELP`
	// header line on the first occurrence of a metric name).
	Description string
}

package snmppoll

// Built-in MIB modules — the metric rail.
//
// Each module embeds, at build time, the OID columns it reads and how each
// maps to an internal metric name. Those names are declared with full
// otel: blocks in the snmp_poll transformer YAML, so they resolve on
// PRTG/Nagios/Prometheus/OTLP and satisfy the #189 mapping guard.
//
// The agent never fetches a MIB at runtime (vendor-neutrality, see
// SNMP-OTEL-MAPPING.md). Topology MIBs (LLDP/BRIDGE-FDB/ipCidrRoute/ARP)
// are NOT here — they ride the entity rail (entity.Source), not the metric
// rail, and land in Lot 5.

// metricKind classifies how a polled value is exposed downstream. It
// mirrors the SNMP SMI base types relevant to monitoring: cumulative
// counters (Counter32/Counter64) map to OTel counters; Gauge32 / Integer
// map to OTel gauges. The transformer YAML carries the authoritative OTel
// type; this kind only drives probe-side tagging.
type metricKind string

const (
	kindCounter metricKind = "counter"
	kindGauge   metricKind = "gauge"
)

// oidMapping describes how one SNMP OID (scalar or table column) becomes
// an internal metric. For table columns Walk is true and the row index is
// appended as an attribute named IndexLabel.
type oidMapping struct {
	// OID is the base OID in dotted form without a leading dot
	// (e.g. "1.3.6.1.2.1.2.2.1.10" for ifInOctets).
	OID string
	// Metric is the internal probe metric name. It MUST match a metric
	// entry in the snmp_poll transformer YAML so the mapper can resolve it.
	Metric string
	// Kind classifies the SNMP value (counter vs gauge).
	Kind metricKind
	// Walk is true for a table column to BulkWalk; the trailing
	// sub-identifier becomes the row index. Scalars set Walk false and are
	// fetched as <OID>.0.
	Walk bool
	// IndexLabel is the attribute key carrying the row index for walked
	// columns (e.g. "if_index"). Empty for scalars.
	IndexLabel string
}

// builtinMIBs maps a built-in MIB selector (as used in the YAML "mibs:"
// list) to its OID mappings. Lot 1b ships MIB-II (system uptime) and
// IF-MIB (interface counters, gauges, status). HOST-RESOURCES-MIB,
// ENTITY-SENSOR-MIB, vendor MIBs, discovery and topology are later lots.
var builtinMIBs = map[string][]oidMapping{
	"mib-2": {
		// sysUpTime.0 — TimeTicks (centiseconds). Gauge; the transformer
		// declares the unit.
		{OID: "1.3.6.1.2.1.1.3", Metric: "snmp.sys.uptime", Kind: kindGauge, Walk: false},
	},
	"if-mib": {
		// ifTable counters — cumulative.
		{OID: "1.3.6.1.2.1.2.2.1.10", Metric: "snmp.interface.in_octets", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.16", Metric: "snmp.interface.out_octets", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.14", Metric: "snmp.interface.in_errors", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.20", Metric: "snmp.interface.out_errors", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.13", Metric: "snmp.interface.in_discards", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.19", Metric: "snmp.interface.out_discards", Kind: kindCounter, Walk: true, IndexLabel: "if_index"},

		// ifSpeed (Gauge32, bits/s) and admin/oper status (INTEGER enum).
		{OID: "1.3.6.1.2.1.2.2.1.5", Metric: "snmp.interface.speed", Kind: kindGauge, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.7", Metric: "snmp.interface.admin_status", Kind: kindGauge, Walk: true, IndexLabel: "if_index"},
		{OID: "1.3.6.1.2.1.2.2.1.8", Metric: "snmp.interface.oper_status", Kind: kindGauge, Walk: true, IndexLabel: "if_index"},
	},
}

// builtinMIBNames returns the set of recognised built-in MIB selectors.
func builtinMIBNames() map[string]bool {
	names := make(map[string]bool, len(builtinMIBs))
	for name := range builtinMIBs {
		names[name] = true
	}
	return names
}

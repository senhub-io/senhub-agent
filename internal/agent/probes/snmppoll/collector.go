package snmppoll

import (
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// familyFor derives the metric_type family (Sensor Builder chip) from the
// internal metric name.
func familyFor(metric string) string {
	switch {
	case strings.HasPrefix(metric, "snmp.interface."):
		return "interface"
	case strings.HasPrefix(metric, "snmp.sys."):
		return "system"
	default:
		return "snmp"
	}
}

// buildPlan flattens the resolved config into the OID mappings to poll,
// expanding each requested built-in MIB and appending custom mappings.
// Custom mappings carrying an IndexLabel are walked; the rest are scalars.
func buildPlan(cfg *config) (scalars, walks []oidMapping) {
	add := func(m oidMapping) {
		if m.Walk {
			walks = append(walks, m)
		} else {
			scalars = append(scalars, m)
		}
	}
	for _, mib := range cfg.MIBs {
		for _, m := range builtinMIBs[mib] {
			add(m)
		}
	}
	for _, c := range cfg.Custom {
		add(oidMapping{
			OID:        c.OID,
			Metric:     c.Metric,
			Kind:       c.Kind,
			Walk:       c.IndexLabel != "",
			IndexLabel: c.IndexLabel,
			Dynamic:    true,
		})
	}
	return scalars, walks
}

// collect runs one poll cycle and returns the decoded datapoints (without
// probe_name/probe_type enrichment — the probe adds those). It is
// best-effort: a failed scalar Get or a failed column walk is logged and
// skipped so one unreadable OID does not drop the whole cycle. The caller
// has already verified reachability (Connect) and emits senhub.snmp.up.
// collect reads the configured OIDs and returns datapoints. deviceID and
// ifNames carry the polled device's resolved network.device.id and its
// ifIndex→ifName map (from the entity source's last sweep); they tag the
// metrics with the SAME identity as the topology entities so a backend joins
// device/interface metrics to their entities. Both may be empty before the
// first topology sweep — the tags are simply omitted until then.
// The boolean result reports whether the device ANSWERED at least one
// request this cycle: UDP "connect" cannot fail for an unreachable or
// auth-failing device, so reachability must be judged on responses —
// otherwise senhub.snmp.up stays 1 through a real outage and the
// SnmpDeviceDown alert never fires.
func collect(client snmpClient, cfg *config, instance, deviceID string, ifNames map[string]string, now time.Time, log *logger.ModuleLogger) ([]data_store.DataPoint, bool) {
	scalars, walks := buildPlan(cfg)
	points := make([]data_store.DataPoint, 0, len(scalars)+len(walks)*8)
	answered := false

	// Scalars: a single (chunked) Get of all <OID>.0 instances.
	if len(scalars) > 0 {
		oids := make([]string, len(scalars))
		byOID := make(map[string]oidMapping, len(scalars))
		for i, m := range scalars {
			scalarOID := m.OID + ".0"
			oids[i] = scalarOID
			byOID[scalarOID] = m
		}
		binds, err := client.Get(oids)
		if err != nil {
			log.Warn().Err(err).Str("target", instance).Msg("SNMP scalar get failed, skipped")
		} else {
			answered = true
			for _, vb := range binds {
				if !vb.IsNumeric {
					continue
				}
				if m, ok := byOID[vb.OID]; ok {
					points = append(points, newPoint(m, vb.Value, now, instance, deviceID, nil))
				}
			}
		}
	}

	// Table columns: one BulkWalk per column, row index → attribute.
	for _, m := range walks {
		binds, err := client.BulkWalk(m.OID)
		if err != nil {
			log.Warn().Err(err).Str("target", instance).Str("oid", m.OID).Str("metric", m.Metric).Msg("SNMP walk failed, skipped")
			continue
		}
		answered = true
		prefix := m.OID + "."
		for _, vb := range binds {
			if !vb.IsNumeric {
				continue
			}
			index := strings.TrimPrefix(vb.OID, prefix)
			if index == "" || index == vb.OID {
				continue
			}
			var extra []tags.Tag
			if m.IndexLabel != "" {
				extra = append(extra, tags.Tag{Key: m.IndexLabel, Value: index})
				// Interface metrics: resolve the ifIndex to interface.name so the
				// datapoint joins to its network.interface entity.
				if m.IndexLabel == "if_index" {
					if name := ifNames[index]; name != "" {
						extra = append(extra, tags.Tag{Key: "interface.name", Value: name})
					}
				}
			}
			points = append(points, newPoint(m, vb.Value, now, instance, deviceID, extra))
		}
	}

	return points, answered
}

func newPoint(m oidMapping, value float64, now time.Time, instance, deviceID string, extra []tags.Tag) data_store.DataPoint {
	t := baseTags(instance, deviceID, m)
	t = append(t, extra...)
	return data_store.DataPoint{
		Name:      metricName(m),
		Timestamp: now,
		Value:     value,
		Tags:      t,
	}
}

// metricName is the datapoint name emitted for a mapping. Built-in module
// metrics keep their internal name (resolved via the transformer YAML).
// Operator custom mappings are emitted under a canonical senhub.snmp.*
// OTel name so the mapper passes them through deterministically (#207).
func metricName(m oidMapping) string {
	if m.Dynamic {
		return dynamicOtelName(m.Metric)
	}
	return m.Metric
}

// dynamicOtelName derives the canonical OTel name for an operator custom
// mapping. The senhub.snmp.* namespace keeps long-tail SNMP objects in a
// deterministic, non-colliding space (SNMP-OTEL-MAPPING.md, Lot 1b). A
// name the operator already namespaced under senhub.* keeps its prefix.
// The result is sanitised to OTel-valid characters so a sloppy config
// value can't produce an unexportable Prometheus/OTLP name.
func dynamicOtelName(metric string) string {
	if strings.HasPrefix(metric, "senhub.") {
		return sanitizeOtelName(metric)
	}
	return "senhub.snmp." + sanitizeOtelName(metric)
}

// sanitizeOtelName replaces any character outside the OTel metric-name set
// (letters, digits, dot, underscore) with an underscore. Dots are kept so
// the hierarchical namespace survives.
func sanitizeOtelName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func baseTags(instance, deviceID string, m oidMapping) []tags.Tag {
	t := []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: familyFor(m.Metric)},
	}
	// network.device.id ties every metric of this device to its network.device
	// entity (same resolved id). Empty before the first topology sweep.
	if deviceID != "" {
		t = append(t, tags.Tag{Key: "network.device.id", Value: deviceID})
	}
	// Dynamic metrics have no transformer YAML row; carry the OTel type so
	// the mapper passes them through with the right counter/gauge semantics.
	if m.Dynamic {
		t = append(t, tags.Tag{Key: "otel_type", Value: string(m.Kind)})
	}
	return t
}

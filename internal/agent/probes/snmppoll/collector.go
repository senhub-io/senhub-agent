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
		})
	}
	return scalars, walks
}

// collect runs one poll cycle and returns the decoded datapoints (without
// probe_name/probe_type enrichment — the probe adds those). It is
// best-effort: a failed scalar Get or a failed column walk is logged and
// skipped so one unreadable OID does not drop the whole cycle. The caller
// has already verified reachability (Connect) and emits senhub.snmp.up.
func collect(client snmpClient, cfg *config, instance string, now time.Time, log *logger.ModuleLogger) []data_store.DataPoint {
	scalars, walks := buildPlan(cfg)
	points := make([]data_store.DataPoint, 0, len(scalars)+len(walks)*8)

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
			for _, vb := range binds {
				if !vb.IsNumeric {
					continue
				}
				if m, ok := byOID[vb.OID]; ok {
					points = append(points, newPoint(m.Metric, vb.Value, now, baseTags(instance, m.Metric)))
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
		prefix := m.OID + "."
		for _, vb := range binds {
			if !vb.IsNumeric {
				continue
			}
			index := strings.TrimPrefix(vb.OID, prefix)
			if index == "" || index == vb.OID {
				continue
			}
			t := baseTags(instance, m.Metric)
			if m.IndexLabel != "" {
				t = append(t, tags.Tag{Key: m.IndexLabel, Value: index})
			}
			points = append(points, newPoint(m.Metric, vb.Value, now, t))
		}
	}

	return points
}

func newPoint(name string, value float64, now time.Time, t []tags.Tag) data_store.DataPoint {
	return data_store.DataPoint{
		Name:      name,
		Timestamp: now,
		Value:     float32(value),
		Tags:      t,
	}
}

func baseTags(instance, metric string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: instance},
		{Key: "metric_type", Value: familyFor(metric)},
	}
}

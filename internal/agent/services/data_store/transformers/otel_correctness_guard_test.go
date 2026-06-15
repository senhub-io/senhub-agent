package transformers

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// TestYAMLDefinitions_OtelTypeUnitNamingGuard turns the OTel-correctness fixes
// of issues #464 (invalid otel.type), #465 (unit suffix in metric names) and
// #466 (ratio gauge left at 0-100) into regression-proof CI checks.
//
// The pre-existing guard (otel_mapping_guard_test.go) only asserts a mapping
// EXISTS; it never validated the mapping's correctness, which is why
// haproxy's `type: sum`, the unit-suffixed names and mssql's `%` ratio all
// passed review. This guard validates the mapping itself.
func TestYAMLDefinitions_OtelTypeUnitNamingGuard(t *testing.T) {
	// otel.type allowed by the OTLP/Prometheus mappers. Anything else (e.g.
	// "sum") is silently downgraded to gauge — #464.
	allowedTypes := map[string]bool{"counter": true, "gauge": true, "updowncounter": true}

	// OTel-first naming forbids encoding the unit in the metric name; the unit
	// lives in otel.unit (UCUM). These suffixes re-encode it — #465.
	unitSuffixes := []string{".bytes", "_bytes", ".seconds", "_seconds", "_total"}

	// Ratio/utilization gauges are dimensionless 0..1 in the convention, so
	// otel.unit MUST be "1" for the mapper to divide a 0-100 source by 100 — #466.
	ratioNameMarkers := []string{"ratio", "utilization", "hit_ratio"}

	// Documented, permanent exceptions to the unit-suffix rule. The guard
	// enforces the rule for everything NOT listed here.
	//   - enterprise / pre-OTel-first legacy probes (live in senhub-agent-
	//     enterprise or predate the rule): citrix, netscaler, veeam, the
	//     ping_*/load_webapp synthetic probes;
	//   - INTENTIONAL contrib-aligned names: the OTel-first rule is "match the
	//     otelcol-contrib receiver name"; docker (dockerstatsreceiver) and
	//     memcached.bytes (memcachedreceiver) end in a unit word UPSTREAM, so
	//     renaming them would diverge from the standard we align to. Kept by
	//     design, not a gap.
	knownUnitSuffixGaps := map[string]bool{
		"senhub.citrix.machines.multi_session_fault_total": true,
		"senhub.probe.http.duration_seconds":               true,
		"senhub.netscaler.compression.bytes":               true,
		"senhub.probe.icmp.duration_seconds":               true,
		"senhub.veeam.job.last_run.bytes":                  true,
		"container.network.io.usage.tx_bytes":              true, // dockerstatsreceiver
		"container.network.io.usage.rx_bytes":              true, // dockerstatsreceiver
		"memcached.bytes":                                  true, // memcachedreceiver
	}

	entries, err := definitionFiles.ReadDir("definitions")
	if err != nil {
		t.Fatalf("read definitions: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := definitionFiles.ReadFile("definitions/" + entry.Name())
		if err != nil {
			t.Errorf("read %s: %v", entry.Name(), err)
			continue
		}
		var def ProbeDefinition
		if err := yaml.Unmarshal(data, &def); err != nil || len(def.Metrics) == 0 {
			continue // non-ProbeDefinition or pure lookup YAML
		}

		for _, m := range def.Metrics {
			if m.Otel == nil || m.Otel.Skip || m.Otel.Name == "" {
				continue // mapping presence is the other guard's job
			}
			name := m.Otel.Name

			// #464 — otel.type must be a mapper-recognised type.
			if m.Otel.Type != "" && !allowedTypes[m.Otel.Type] {
				t.Errorf("%s / %s: otel.type %q is not one of counter|gauge|updowncounter "+
					"(it is silently downgraded to gauge, losing monotonic/cumulative semantics) — #464",
					def.ProbeName, name, m.Otel.Type)
			}

			// #465 — no unit suffix in the OTel metric name.
			for _, suf := range unitSuffixes {
				if strings.HasSuffix(name, suf) && !knownUnitSuffixGaps[name] {
					t.Errorf("%s / %s: OTel metric name carries the unit suffix %q — "+
						"the unit belongs in otel.unit, not the name — #465", def.ProbeName, name, suf)
				}
			}

			// #466 — ratio/utilization gauges must be otel.unit "1".
			lname := strings.ToLower(name)
			isRatio := false
			for _, mark := range ratioNameMarkers {
				if strings.HasSuffix(lname, mark) {
					isRatio = true
					break
				}
			}
			if isRatio && m.Otel.Type == "gauge" && m.Otel.Unit != "1" {
				t.Errorf("%s / %s: ratio/utilization gauge has otel.unit %q, want \"1\" "+
					"(else a 0-100 source is never divided to the 0-1 ratio the convention "+
					"mandates) — #466", def.ProbeName, name, m.Otel.Unit)
			}
		}
	}
}

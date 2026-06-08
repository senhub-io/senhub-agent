package transformers

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// TestYAMLDefinitions_EveryMetricHasOtelMappingOrSkip is the structural guard
// behind issue #189. For every metric declared in a probe transformer YAML
// (the contract for what Collect() emits — see .claude/rules/data-store.md),
// it asserts the metric either:
//
//   - resolves to an OTel mapping (an `otel:` block carrying a `name:`), or
//   - is explicitly opted out with `otel.skip: true` (and a `reason:`).
//
// This is the exact decision otelmapper.Resolve makes (resolve.go lines
// 37-42): a metric with no `otel:` block — or an `otel:` block with neither a
// name nor skip — produces a "metric has no OTel mapping" error and is
// silently dropped on the OTLP/Prometheus path while still flowing to PRTG.
// That is the #137 swap_* failure mode: visible at runtime as a
// "Metric has no OTel mapping — not exported" warning, caught by no test.
//
// A bare `otel.skip: true` without a `reason:` is also flagged: every
// intentional non-export must document why, so the skip is an auditable
// decision rather than a silent escape hatch (matches the existing
// reason-carrying skips in event/syslog/ibmi/redfish YAMLs).
func TestYAMLDefinitions_EveryMetricHasOtelMappingOrSkip(t *testing.T) {
	entries, err := definitionFiles.ReadDir("definitions")
	if err != nil {
		t.Fatalf("read definitions: %v", err)
	}

	type violation struct {
		probe  string
		metric string
		why    string
	}
	var violations []violation

	record := func(probe string, m MetricDefinition) {
		if why := otelMappingViolation(m); why != "" {
			violations = append(violations, violation{probe: probe, metric: m.Name, why: why})
		}
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := entry.Name()
		data, err := definitionFiles.ReadFile("definitions/" + name)
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}

		var def ProbeDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			// Some YAMLs (nagios.yaml, lookups.yaml, …) intentionally don't
			// match ProbeDefinition. They carry no metrics block; skip rather
			// than fail — mirrors yaml_lint_test.go.
			t.Logf("skip %s (not a ProbeDefinition): %v", name, err)
			continue
		}

		// Pure lookup / shared YAMLs have no metrics to guard.
		if len(def.Metrics) == 0 {
			continue
		}

		probeID := strings.TrimSuffix(name, ".yaml")
		for _, m := range def.Metrics {
			record(probeID, m)
		}
	}

	if len(violations) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("found probe metrics with no OTel mapping and no explicit skip ")
	b.WriteString("(see issue #189 — these are exported by PRTG/HTTP but silently dropped by OTLP/Prometheus):\n")
	for _, v := range violations {
		b.WriteString("  ")
		b.WriteString(v.probe)
		b.WriteString(".")
		b.WriteString(v.metric)
		b.WriteString(": ")
		b.WriteString(v.why)
		b.WriteByte('\n')
	}
	t.Fatal(b.String())
}

// otelMappingViolation returns a non-empty explanation when a metric
// definition would be silently dropped by the OTel-aware sinks. It encodes
// the same decision otelmapper.Resolve makes; keeping it here (rather than
// importing otelmapper) avoids an import cycle and keeps the guard co-located
// with the YAML it reads. An empty return means the metric is well-formed
// (mapped, or skipped with a reason). A metric with no internal name is not
// this guard's concern (returns "").
func otelMappingViolation(m MetricDefinition) string {
	if m.Name == "" {
		return ""
	}
	switch {
	case m.Otel == nil:
		return "no `otel:` block — emitted to PRTG/HTTP but silently dropped by OTLP/Prometheus; add an `otel:` mapping or `otel.skip: true` with a reason"
	case m.Otel.Skip:
		if strings.TrimSpace(m.Otel.Reason) == "" {
			return "`otel.skip: true` without a `reason:` — document why the metric is intentionally not exported"
		}
		return ""
	case m.Otel.Name == "":
		return "`otel:` block has neither a `name:` nor `skip: true` — Resolve will error and the metric is dropped"
	default:
		return ""
	}
}

// TestOtelMappingViolation_Predicate proves the guard is not vacuous: it
// flags exactly the unmapped shapes (the #137 swap_* class) and passes the
// well-formed ones. If this ever goes green for an unmapped metric, the
// file-walk guard above would silently let real drift through.
func TestOtelMappingViolation_Predicate(t *testing.T) {
	cases := []struct {
		name     string
		metric   MetricDefinition
		wantFlag bool
	}{
		{
			name:     "unmapped metric (the #137 swap_* shape)",
			metric:   MetricDefinition{Name: "swap_used", Unit: "By"},
			wantFlag: true,
		},
		{
			name:     "otel block present but no name and no skip",
			metric:   MetricDefinition{Name: "foo", Otel: &OtelMapping{Unit: "By"}},
			wantFlag: true,
		},
		{
			name:     "skip without reason",
			metric:   MetricDefinition{Name: "foo", Otel: &OtelMapping{Skip: true}},
			wantFlag: true,
		},
		{
			name:     "mapped metric",
			metric:   MetricDefinition{Name: "swap_used", Otel: &OtelMapping{Name: "system.paging.usage", Unit: "By", Type: "gauge"}},
			wantFlag: false,
		},
		{
			name:     "skip with reason",
			metric:   MetricDefinition{Name: "foo", Otel: &OtelMapping{Skip: true, Reason: "event conduit, not a metric"}},
			wantFlag: false,
		},
		{
			name:     "no internal name is out of scope",
			metric:   MetricDefinition{Otel: nil},
			wantFlag: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := otelMappingViolation(tc.metric) != ""
			if got != tc.wantFlag {
				t.Fatalf("otelMappingViolation(%+v) flagged=%v, want %v", tc.metric, got, tc.wantFlag)
			}
		})
	}
}

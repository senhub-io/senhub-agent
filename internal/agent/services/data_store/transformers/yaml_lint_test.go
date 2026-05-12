package transformers

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// TestYAMLDefinitions_HaveProbeSideUnitForOtelConvertibleMetrics enforces
// the contract behind data_store.applyUnitCorrections + otelmapper.convertValue:
//
//	any metric exposing an `otel:` block whose `unit` differs from the
//	probe-side `unit` requires the probe-side `unit` to be declared so
//	the conversion (%→ratio, MB→By, ms→s, …) can fire.
//
// Without this guard, a new metric added with `unit: "%"` (or any other
// non-OTel-native unit) and no probe-side unit declaration would
// silently emit wrong values on the OTLP path (raw percent stored as
// "ratio", etc.). Caught at CI rather than after deployment.
//
// Metrics that explicitly skip OTel emission (`otel.skip: true`) or
// declare an OTel unit equal to the probe-side unit are exempt.
func TestYAMLDefinitions_HaveProbeSideUnitForOtelConvertibleMetrics(t *testing.T) {
	entries, err := definitionFiles.ReadDir("definitions")
	if err != nil {
		t.Fatalf("read definitions: %v", err)
	}

	type violation struct {
		probe      string
		metric     string
		probeUnit  string
		otelUnit   string
		hint       string
	}
	var violations []violation

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := entry.Name()
		// Shared lookup / shared YAMLs (no metrics block in the
		// expected shape) get a separate sanity check.
		data, err := definitionFiles.ReadFile("definitions/" + name)
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}

		var def ProbeDefinition
		if err := yaml.Unmarshal(data, &def); err != nil {
			// Some YAMLs (e.g. nagios.yaml, storage.yaml) intentionally
			// don't match ProbeDefinition. Skip rather than fail —
			// they're not metric definitions.
			t.Logf("skip %s (not a ProbeDefinition): %v", name, err)
			continue
		}

		if len(def.Metrics) == 0 {
			continue
		}

		probeID := strings.TrimSuffix(name, ".yaml")
		for _, m := range def.Metrics {
			if m.Otel == nil || m.Otel.Skip {
				continue
			}
			// If the probe-side unit equals the OTel unit, no
			// conversion is required and a missing probe-side unit
			// would be benign (same number flows through). Still flag
			// missing probe-side unit declarations though, because:
			//   - It's a documentation gap that future-confuses readers.
			//   - The OTel unit is on its own a hint about the
			//     probe-side magnitude.
			if m.Unit == "" && m.Otel.Unit != "" {
				violations = append(violations, violation{
					probe:     probeID,
					metric:    m.Name,
					probeUnit: m.Unit,
					otelUnit:  m.Otel.Unit,
					hint:      "declare `unit:` matching what the probe emits (will be enriched as the `unit` tag on every DataPoint)",
				})
			}
		}
	}

	if len(violations) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("found probe metrics with an otel: block but no probe-side `unit:` declaration:\n")
	for _, v := range violations {
		b.WriteString("  ")
		b.WriteString(v.probe)
		b.WriteString(".")
		b.WriteString(v.metric)
		b.WriteString(": otel.unit=")
		b.WriteString(v.otelUnit)
		b.WriteString(" — ")
		b.WriteString(v.hint)
		b.WriteByte('\n')
	}
	t.Fatal(b.String())
}

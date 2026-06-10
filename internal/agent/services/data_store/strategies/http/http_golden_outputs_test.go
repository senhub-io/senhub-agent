package http

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var updateGolden = flag.Bool("update", false, "rewrite golden output files")

// goldenProbes is the FREE wedge set the campaign exercises. Each
// probe's full definition is rendered through the real ingestion +
// conversion path and compared against a checked-in golden file, so
// any unit/channel/format regression surfaces as a diff (#316 item 1).
var goldenProbes = []string{"cpu", "memory", "network", "logicaldisk", "snmp_poll"}

// TestGoldenOutputs_PRTG renders every metric of each probe definition
// to PRTG channels and compares against testdata/golden/<probe>_prtg.golden.
// Regenerate deliberately with: go test -run TestGoldenOutputs -update
func TestGoldenOutputs_PRTG(t *testing.T) {
	for _, probe := range goldenProbes {
		t.Run(probe, func(t *testing.T) {
			converter, cache := newGoldenHarness(t, probe)
			_ = cache

			channels := converter.GetMetricsForProbe(probe)
			if len(channels) == 0 {
				t.Fatalf("no PRTG channels rendered for %s", probe)
			}
			sort.Slice(channels, func(i, j int) bool { return channels[i].Channel < channels[j].Channel })

			// No rendered channel may leak a literal {placeholder}
			// (#317: per-device snmp_poll channels rendered
			// "SNMP {instance} Reachability").
			for _, ch := range channels {
				if strings.ContainsAny(ch.Channel, "{}") {
					t.Errorf("channel name leaks an unsubstituted placeholder: %q", ch.Channel)
				}
			}

			rendered, err := json.MarshalIndent(channels, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", "golden", probe+"_prtg.golden"), rendered)
		})
	}
}

// TestGoldenOutputs_NagiosChecks evaluates the default (embedded)
// Nagios checks that target each probe against the same fixture and
// compares the rendered check lines against
// testdata/golden/<probe>_nagios.golden.
func TestGoldenOutputs_NagiosChecks(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	config := cm.LoadNagiosConfig()

	for _, probe := range goldenProbes {
		t.Run(probe, func(t *testing.T) {
			converter, cache := newGoldenHarness(t, probe)
			processor := NewMetricsProcessor(cache, converter, nil, newTestLogger())

			var lines []string
			for _, check := range config.Checks {
				if check.ProbeFilter != "" && check.ProbeFilter != probe {
					continue
				}
				metrics := cache.GetProbeMetrics(probe)
				for _, metricDef := range check.Metrics {
					result := processor.ProcessNagiosMetric(metricDef, metrics, NagiosOverrides{})
					lines = append(lines, fmt.Sprintf("%s/%s status=%d %s | %s",
						check.Name, metricDef.Channel, result.Status, result.Message, result.PerfData))
				}
			}
			if len(lines) == 0 {
				t.Skipf("no default Nagios checks target probe %s", probe)
			}
			sort.Strings(lines)
			compareGolden(t, filepath.Join("testdata", "golden", probe+"_nagios.golden"), []byte(strings.Join(lines, "\n")+"\n"))
		})
	}
}

// TestNagiosPerfData_RateUOMSurvives pins #316 item 5 for the rate
// channels: a B/s display unit must reach the perfdata UOM untouched.
func TestNagiosPerfData_RateUOMSurvives(t *testing.T) {
	processor := NewMetricsProcessor(nil, nil, nil, newTestLogger())
	perf := processor.buildPerfData("bytes_received", 1234.5, "100", "500", "B/s")
	if !strings.Contains(perf, "B/s;") {
		t.Errorf("perfdata lost the rate UOM: %q", perf)
	}
}

// newGoldenHarness ingests one synthetic datapoint per definition
// metric of the probe through the REAL cache ingestion path
// (AddDataPointsWithTransformer), so unit resolution, time-series keys
// and discriminant handling match production.
func newGoldenHarness(t *testing.T, probe string) (*FormatConverter, *MetricCache) {
	t.Helper()
	registry := transformers.NewTransformerRegistry(createTestLogger())
	cache := NewMetricCache(5*time.Minute, newTestLogger())
	converter := NewFormatConverter(registry, newTestLogger(), cache)

	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}
	probeDef, ok := defs[probe]
	if !ok {
		t.Fatalf("no embedded definition for probe %s", probe)
	}
	metricDefs := probeDef.Metrics

	points := make([]datapoint.DataPoint, 0, len(metricDefs))
	seen := map[string]bool{}
	for i, m := range metricDefs {
		if m.Name == "" || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		dpTags := []tags.Tag{
			{Key: "probe_name", Value: probe},
			{Key: "probe_type", Value: probe},
		}
		// Multi-instance metrics need their labels present so channel
		// templates expand instead of leaking "{placeholder}" — both
		// the metric's own labels and the definition-level defaults
		// (the production probes tag every datapoint the same way).
		for _, label := range append(append([]string{}, m.MultiInstanceLabels...), probeDef.MultiInstanceLabels...) {
			dpTags = append(dpTags, tags.Tag{Key: label, Value: "t" + label})
		}
		points = append(points, datapoint.DataPoint{
			Name:      m.Name,
			Timestamp: time.Now(),
			Value:     float32(i%7)*11.5 + 42, // fixed, varied, deterministic
			Tags:      dpTags,
		})
	}
	cache.AddDataPointsWithTransformer(points, registry)
	return converter, cache
}

func compareGolden(t *testing.T, path string, rendered []byte) {
	t.Helper()
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", path, err)
	}
	if string(want) != string(rendered) {
		t.Errorf("output differs from %s — if the contract change is deliberate, regenerate with -update.\n--- got ---\n%s\n--- want ---\n%s",
			path, truncateForDiff(rendered), truncateForDiff(want))
	}
}

func truncateForDiff(b []byte) string {
	const max = 4000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "\n...(truncated)"
}

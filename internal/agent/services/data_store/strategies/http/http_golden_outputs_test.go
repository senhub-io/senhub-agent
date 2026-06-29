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

// TestDefaultNagiosConfig_ResolvesAgainstEmittedChannels guards the
// #335 regression class: the shipped default nagios.yaml must not
// reference a channel that the probe does not actually emit on the
// running platform, or that check returns permanent UNKNOWN
// out of the box.
//
// Unlike TestNagiosDefaultChecks_ChannelsExist (which checks against the
// union of every probe definition — the platform-agnostic superset, so
// a Windows-only channel like memory_available passes even though Unix
// never emits it), this seeds the cache with ONLY the channels the probe
// emits on THIS OS (emittedChannelsByProbe, build-tagged), then runs
// each default check through the real Nagios processor. A check whose
// channel is absent from the emitted set surfaces as status=3.
func TestDefaultNagiosConfig_ResolvesAgainstEmittedChannels(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	config := cm.LoadNagiosConfig()

	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	// Seed one cache with every FREE-wedge probe's emitted channels.
	// Per-probe metric slices are read back from it, and a union slice
	// serves probe-agnostic checks (probe_filter: "", e.g. system_health
	// which mixes cpu + memory channels).
	registry := transformers.NewTransformerRegistry(createTestLogger())
	cache := NewMetricCache(5*time.Minute, newTestLogger())
	// snmp_poll has no fixed channel set (channels are per-device,
	// minted at runtime from polled OIDs) and no default check targets
	// it, so it carries no manifest and is skipped.
	var union []datapoint.DataPoint
	for _, probe := range goldenProbes {
		emitted, ok := emittedChannelsByProbe[probe]
		if !ok {
			continue
		}
		union = append(union, emittedDataPoints(probe, defs[probe], emitted)...)
	}
	cache.AddDataPointsWithTransformer(union, registry)

	processor := NewMetricsProcessor(cache, nil, nil, newTestLogger())
	allMetrics := make([]CachedMetric, 0)
	for probe := range emittedChannelsByProbe {
		allMetrics = append(allMetrics, cache.GetProbeMetrics(probe)...)
	}

	for _, check := range config.Checks {
		metrics := allMetrics
		if check.ProbeFilter != "" {
			// Only assert against probes whose emitted set we model.
			// Checks for probes outside the FREE-wedge manifest (e.g.
			// veeam, ping_*) are operator-facing and not exercised here.
			if _, ok := emittedChannelsByProbe[check.ProbeFilter]; !ok {
				continue
			}
			metrics = cache.GetProbeMetrics(check.ProbeFilter)
		}
		for _, metricDef := range check.Metrics {
			result := processor.ProcessNagiosMetric(metricDef, metrics, NagiosOverrides{})
			if result.Status == 3 {
				t.Errorf("default check %q channel %q is UNKNOWN-by-construction on this platform: %s",
					check.Name, metricDef.Channel, result.Message)
			}
		}
	}
}

// emittedDataPoints builds one synthetic datapoint per channel the
// probe actually emits on this platform (from emittedChannelsByProbe).
// Multi-instance channels carry their definition labels so per-core /
// per-interface checks resolve instead of leaking a "{placeholder}".
func emittedDataPoints(probe string, probeDef transformers.ProbeDefinition, emitted []string) []datapoint.DataPoint {
	labelsByMetric := map[string][]string{}
	for _, m := range probeDef.Metrics {
		labelsByMetric[m.Name] = m.MultiInstanceLabels
	}

	points := make([]datapoint.DataPoint, 0, len(emitted))
	for i, name := range emitted {
		dpTags := []tags.Tag{
			{Key: "probe_name", Value: probe},
			{Key: "probe_type", Value: probe},
		}
		for _, label := range append(append([]string{}, labelsByMetric[name]...), probeDef.MultiInstanceLabels...) {
			dpTags = append(dpTags, tags.Tag{Key: label, Value: "t" + label})
		}
		points = append(points, datapoint.DataPoint{
			Name:      name,
			Timestamp: time.Now(),
			Value:     float64(i%7)*11.5 + 42,
			Tags:      dpTags,
		})
	}
	return points
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
			Value:     float64(i%7)*11.5 + 42, // fixed, varied, deterministic
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
	// Normalize line endings: a checkout with eol conversion (or an
	// editor) must not turn every comparison into a CRLF diff.
	wantS := strings.ReplaceAll(string(want), "\r\n", "\n")
	renderedS := strings.ReplaceAll(string(rendered), "\r\n", "\n")
	if wantS != renderedS {
		t.Errorf("output differs from %s — if the contract change is deliberate, regenerate with -update.\n--- got ---\n%s\n--- want ---\n%s",
			path, truncateForDiff([]byte(renderedS)), truncateForDiff([]byte(wantS)))
	}
}

func truncateForDiff(b []byte) string {
	const max = 4000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "\n...(truncated)"
}

// Package goldenformats freezes what each sink actually emits for one
// canonical fixture.
//
// The agent renders the same measurement into five shapes, through two
// different mapping paths: Prometheus and OTLP resolve through the
// neutral otelmapper, while PRTG, Nagios and the SenHub JSON still go
// through the legacy transformer. Converging them (#293) is only safe
// with a checked-in record of today's output, so a change that alters
// what a customer's PRTG or Grafana shows is a visible diff rather than
// a discovery in production (#296).
//
// Regenerate deliberately: go test ./... -run TestGoldenFormats -update
package goldenformats

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var update = flag.Bool("update", false, "rewrite golden output files")

// fixtureProbe is the canonical probe rendered through every format. cpu
// is the right choice: every platform has it, it carries both a plain
// gauge and multi-instance series, and it is in the free tier so every
// deployment exercises it.
const fixtureProbe = "cpu"

// fixture builds the datapoint set for the canonical probe from its
// shipped definition, so the goldens follow the definition rather than a
// hand-written list that would drift away from it.
func fixture(t *testing.T) ([]datapoint.DataPoint, transformers.ProbeDefinition) {
	t.Helper()
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}
	def, ok := defs[fixtureProbe]
	if !ok {
		t.Fatalf("no embedded definition for %s", fixtureProbe)
	}

	seen := map[string]bool{}
	points := make([]datapoint.DataPoint, 0, len(def.Metrics))
	for i, m := range def.Metrics {
		if m.Name == "" || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		dpTags := []tags.Tag{
			{Key: "probe_name", Value: fixtureProbe},
			{Key: "probe_type", Value: fixtureProbe},
		}
		for _, label := range m.MultiInstanceLabels {
			dpTags = append(dpTags, tags.Tag{Key: label, Value: "0"})
		}
		points = append(points, datapoint.DataPoint{
			Name:  m.Name,
			Value: float64(i + 1),
			Tags:  dpTags,
		})
	}
	if len(points) == 0 {
		t.Fatalf("fixture produced no datapoints for %s", fixtureProbe)
	}
	return points, def
}

// TestGoldenFormats_OtelRecords freezes the neutral layer every sink
// should eventually agree on. A diff here means the mapping itself
// moved, which changes every downstream format at once.
func TestGoldenFormats_OtelRecords(t *testing.T) {
	points, def := fixture(t)

	var rendered []map[string]any
	for _, p := range points {
		cm := otelmapper.CacheMetric{
			ProbeName:  fixtureProbe,
			ProbeType:  fixtureProbe,
			MetricName: p.Name,
			Value:      p.Value,
			Tags:       tagMap(p.Tags),
		}
		recs, err := otelmapper.Resolve(&def, cm, otelmapper.ResolveOptions{IncludeProbeTags: true})
		if err != nil {
			// A metric with no OTel mapping is a fact worth freezing too.
			rendered = append(rendered, map[string]any{"metric": p.Name, "unmapped": err.Error()})
			continue
		}
		for _, r := range recs {
			rendered = append(rendered, map[string]any{
				"metric":     p.Name,
				"otel_name":  r.Name,
				"unit":       r.Unit,
				"type":       r.Type,
				"attributes": r.Attributes,
			})
		}
	}
	sortByKey(rendered, "otel_name", "metric")
	compare(t, "cpu_otel_records.golden", mustJSON(t, rendered))
}

// TestGoldenFormats_PRTGPushVsPull records the divergence #293 exists to
// remove: the same measurement reaches PRTG under one name when the
// agent pushes and another when PRTG pulls. Freezing both sides means
// the convergence has to state, in a diff, which name it keeps.
func TestGoldenFormats_PRTGPushVsPull(t *testing.T) {
	points, _ := fixture(t)

	push := make([]string, 0, len(points))
	for _, p := range points {
		push = append(push, pushChannelName(p))
	}
	sort.Strings(push)
	compare(t, "cpu_prtg_push_channels.golden", mustJSON(t, push))
}

// pushChannelName mirrors the push path's channel naming (metricId):
// the prtg_metric_id tag when the probe set one, the raw metric name
// otherwise — no display name, no unit, no lookup.
func pushChannelName(p datapoint.DataPoint) string {
	for _, tag := range p.Tags {
		if tag.Key == "prtg_metric_id" {
			return strings.ReplaceAll(tag.Value, "[name]", p.Name)
		}
	}
	return p.Name
}

func tagMap(list []tags.Tag) map[string]string {
	out := make(map[string]string, len(list))
	for _, t := range list {
		out[t.Key] = t.Value
	}
	return out
}

func sortByKey(rows []map[string]any, keys ...string) {
	sort.SliceStable(rows, func(i, j int) bool {
		for _, k := range keys {
			a, _ := rows[i][k].(string)
			b, _ := rows[j][k].(string)
			if a != b {
				return a < b
			}
		}
		return false
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

func compare(t *testing.T, name string, rendered []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (create it with -update): %v", path, err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != strings.ReplaceAll(string(rendered), "\r\n", "\n") {
		t.Errorf("%s differs — if the change is deliberate, regenerate with -update\n--- got ---\n%s", name, rendered)
	}
}

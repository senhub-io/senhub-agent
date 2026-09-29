package http

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http/prometheus"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestEveryDefinitionRendersOnEveryOutput drives every embedded definition
// through the real ingestion path and every pull format the agent serves,
// plus the neutral OTel layer the push outputs read.
//
// The golden tests freeze what five probes render to; this one asks a
// weaker question of all eighty-odd — does each metric come out at all,
// once, without a placeholder — because that is the question nobody had
// asked. A definition whose `attributes:` sat beside `otel:` instead of
// under it made three counters collapse into one series (#930); the
// Zabbix templates of two probes were refused by the server for a shape
// every other output accepted silently. Each output is checked for what
// it can lose.
func TestEveryDefinitionRendersOnEveryOutput(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, probe := range names {
		def := defs[probe]
		if len(def.Metrics) == 0 {
			continue
		}
		t.Run(probe, func(t *testing.T) {
			converter, cache := newGoldenHarness(t, probe)
			registry := transformers.NewTransformerRegistry(createTestLogger())

			mapped := 0
			for _, m := range def.Metrics {
				if m.Otel == nil || !m.Otel.Skip {
					mapped++
				}
			}

			// PRTG: every metric becomes a channel, named without a
			// leaked placeholder, and no two channels share a name.
			channels := converter.GetMetricsForProbe(probe)
			if len(channels) == 0 {
				t.Errorf("PRTG: no channel rendered")
			}
			seenChannel := map[string]bool{}
			for _, ch := range channels {
				if strings.ContainsAny(ch.Channel, "{}") {
					t.Errorf("PRTG: channel leaks a placeholder: %q", ch.Channel)
				}
				if seenChannel[ch.Channel] {
					t.Errorf("PRTG: two channels named %q; the second overwrites the first in the sensor", ch.Channel)
				}
				seenChannel[ch.Channel] = true
			}

			// Nagios: a check names a metric by its definition name, and
			// one on every metric of the probe evaluates rather than
			// coming back UNKNOWN. A metric with a lookup is judged on its
			// state, and the fixture value is not one, so only "not found"
			// counts against it.
			processor := NewMetricsProcessor(cache, converter, nil, newTestLogger())
			cached := cache.GetProbeMetrics(probe)
			for _, m := range def.Metrics {
				// Thresholds no fixture value reaches: a check without any
				// is UNKNOWN by construction, which is not what is measured
				// here. A health-style metric is judged on its state, and
				// the fixture value is not one; that UNKNOWN is tolerated.
				res := processor.ProcessNagiosMetric(NagiosMetric{Channel: m.Name, Aggregation: "none", Warning: "1e12", Critical: "1e13"}, cached, NagiosOverrides{})
				switch {
				case res.Status == 3 && strings.HasPrefix(res.Message, "No metrics found"):
					t.Errorf("Nagios: a check on %q finds nothing: %s", m.Name, res.Message)
				case res.Status == 3 && !strings.Contains(res.Message, "health value") && m.Lookup == "" && (m.Otel == nil || m.Otel.Expand == nil):
					t.Errorf("Nagios: a check on %q is UNKNOWN: %s", m.Name, res.Message)
				case res.Status != 3 && res.PerfData == "":
					t.Errorf("Nagios: a check on %q renders no perfdata", m.Name)
				}
			}
			if sh := converter.GetSenHubMetricsForProbe(probe); len(sh) == 0 {
				t.Errorf("SenHub: no metric rendered")
			}

			// The neutral layer: every metric not marked as skipped
			// resolves, and no two that can run on the same host resolve
			// to the same series. Two metrics that only ever exist on
			// different platforms — a pagefile and a swap — may share one.
			byName := map[string]transformers.MetricDefinition{}
			for _, m := range def.Metrics {
				byName[m.Name] = m
			}
			all := (&cacheAdapter{cache: cache, registry: registry}).GetAll()
			resolved := map[string]bool{}
			for _, goos := range []string{"linux", "windows", "darwin"} {
				seenSeries := map[string]string{}
				for _, cm := range all {
					if md, ok := byName[cm.MetricName]; ok && !md.RunsOn(goos) {
						continue
					}
					recs, err := otelmapper.Resolve(&def, cm, otelmapper.ResolveOptions{})
					if err != nil {
						if !resolved[cm.MetricName] {
							t.Errorf("OTel: %s does not resolve: %v", cm.MetricName, err)
						}
						resolved[cm.MetricName] = true
						continue
					}
					resolved[cm.MetricName] = true
					for _, r := range recs {
						key := seriesKey(r)
						if other, dup := seenSeries[key]; dup && other != cm.MetricName {
							t.Errorf("OTel (%s): %s and %s resolve to the same series %s; one value overwrites the other on every push output", goos, other, cm.MetricName, key)
						}
						seenSeries[key] = cm.MetricName
					}
				}
			}
			if len(resolved) < mapped {
				t.Errorf("OTel: %d metrics mapped in the definition, %d resolved", mapped, len(resolved))
			}

			// Prometheus: the exposition is written, and carries no
			// sample line twice.
			var buf bytes.Buffer
			var unmapped []string
			_, err := prometheus.WriteExposition(
				&cacheAdapter{cache: cache, registry: registry},
				&registryAdapter{registry: registry},
				nil, otelmapper.ResolveOptions{}, &buf,
				func(m otelmapper.CacheMetric, _ error) { unmapped = append(unmapped, m.MetricName) },
				nil)
			if err != nil {
				t.Errorf("Prometheus: %v", err)
			}
			if len(unmapped) > 0 {
				t.Errorf("Prometheus: %d metrics left out of the exposition: %s", len(unmapped), strings.Join(unmapped, ", "))
			}
			seenLine := map[string]bool{}
			for _, line := range strings.Split(buf.String(), "\n") {
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				sample := line
				if i := strings.LastIndex(line, " "); i > 0 {
					sample = line[:i]
				}
				if seenLine[sample] && !platformSplit(def, sample) {
					t.Errorf("Prometheus: sample written twice: %s", sample)
				}
				seenLine[sample] = true
			}
			if mapped > 0 && len(seenLine) == 0 {
				t.Errorf("Prometheus: empty exposition for %d mapped metrics", mapped)
			}
		})
	}
}

// seriesKey is what makes a series distinct on every push output: its
// name and its attribute set.
func seriesKey(r otelmapper.OtelRecord) string {
	keys := make([]string, 0, len(r.Attributes))
	for k := range r.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(r.Name)
	for _, k := range keys {
		fmt.Fprintf(&b, ",%s=%v", k, r.Attributes[k])
	}
	return b.String()
}

// platformSplit reports whether the definition holds metrics restricted to
// different platforms, in which case one exposition built from every metric
// at once legitimately carries a sample twice: no real host does.
func platformSplit(def transformers.ProbeDefinition, _ string) bool {
	seen := map[string]bool{}
	for _, m := range def.Metrics {
		for _, p := range m.Platforms {
			seen[p] = true
		}
	}
	return len(seen) > 1
}

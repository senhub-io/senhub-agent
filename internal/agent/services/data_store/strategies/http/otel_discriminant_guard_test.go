package http

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TestEveryMultiInstanceProbeHasDiscriminantTags turns the #459 data-loss fix
// into a regression-proof check: a probe whose transformer YAML declares
// multi_instance_labels (it emits several series under one OTel metric name,
// distinguished by tags) MUST have a DiscriminantTagsRegistry entry. Without
// it the HTTP cache keys only on the probe+metric name and silently collapses
// every instance onto one slot on the PRTG/Nagios pull sinks — invisible to a
// make test that exercises only the OTLP/Prometheus push path (which keys on
// the full tag set).
func TestEveryMultiInstanceProbeHasDiscriminantTags(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("load transformer definitions: %v", err)
	}

	// Documented baseline: probes with multi_instance_labels but no registry
	// entry today. The guard enforces the rule for everything NOT listed here.
	//   - load_webapp / ping_webapp: enterprise/synthetic legacy probes;
	//   - wifi_signal_strength: in-tree gap the #459 sweep missed, tracked in #481.
	knownDiscriminantGaps := map[string]bool{
		"load_webapp":          true,
		"ping_webapp":          true,
		"wifi_signal_strength": true, // #481
	}

	for name, def := range defs {
		if knownDiscriminantGaps[def.ProbeName] {
			continue
		}
		// Collect the multi_instance labels declared at the probe level and
		// on any individual metric.
		hasMultiInstance := len(def.MultiInstanceLabels) > 0
		for _, m := range def.Metrics {
			if len(m.MultiInstanceLabels) > 0 {
				hasMultiInstance = true
				break
			}
		}
		if !hasMultiInstance {
			continue
		}

		// def keys on probe_name; the registry keys on the probe type, which
		// is the same identifier.
		if entry, ok := DiscriminantTagsRegistry[def.ProbeName]; !ok || len(entry) == 0 {
			t.Errorf("probe %q declares multi_instance_labels but has no (or an empty) "+
				"DiscriminantTagsRegistry entry — its per-instance series collapse to one "+
				"cache slot on the PRTG/Nagios pull sinks (#459). Add the raw discriminant "+
				"tag keys to DiscriminantTagsRegistry in http_cache.go.", name)
		}
	}
}

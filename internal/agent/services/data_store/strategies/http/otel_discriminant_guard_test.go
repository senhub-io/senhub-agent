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

	// Documented baseline: enterprise/synthetic legacy probes with
	// multi_instance_labels but no registry entry. The guard enforces the rule
	// for everything NOT listed here.
	knownDiscriminantGaps := map[string]bool{
		"load_webapp": true,
		"ping_webapp": true,
	}

	for name, def := range defs {
		if knownDiscriminantGaps[def.ProbeName] {
			continue
		}
		// Probes that key on their FULL tag set (arbitrary, externally
		// controlled label sets) never collapse — full-tag keying is a
		// stronger guarantee than any discriminant list, so they are
		// deliberately absent from DiscriminantTagsRegistry.
		if fullTagKeyProbes[def.ProbeName] {
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

// TestEveryDeclaredDimensionIsRegistered closes the hole the guard above
// leaves: it proves a probe is registered, not that what splits its
// series is. azure_container_apps was registered on metric_type alone,
// and when subscription discovery made it follow several applications
// the new azure_app dimension went unregistered. The probe still had an
// entry, so the guard above stayed green while the cache kept one
// application's state and dropped the other five on every pull sink.
//
// A label that describes an instance without splitting it does not need
// registering, but the two errors do not cost the same. A label left
// out because it looked descriptive and was not costs a whole family of
// series, silently, on every pull sink; one registered although its
// value is fixed for the instance its identifier names creates no
// series at all, and the key is internal — the channel an operator
// reads is built from the metric's own tags. So where the reading is
// not certain, the label is registered and the reason written beside
// it in http_cache.go. An entry that stops matching fails this test,
// the way the documented-key guard works.
func TestEveryDeclaredDimensionIsRegistered(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("load transformer definitions: %v", err)
	}

	for _, def := range defs {
		entry, registered := DiscriminantTagsRegistry[def.ProbeName]
		if !registered || fullTagKeyProbes[def.ProbeName] {
			// The guard above already rules on an absent entry, and a
			// full-tag-keyed probe never collapses.
			continue
		}
		known := make(map[string]bool, len(entry))
		for _, tag := range entry {
			known[tag] = true
		}
		declared := append([]string{}, def.MultiInstanceLabels...)
		for _, m := range def.Metrics {
			declared = append(declared, m.MultiInstanceLabels...)
		}
		var missing []string
		seen := map[string]bool{}
		for _, label := range declared {
			if label == "" || known[label] || seen[label] {
				continue
			}
			seen[label] = true
			missing = append(missing, label)
		}
		if len(missing) == 0 {
			continue
		}
		t.Errorf("probe %q splits its series on %v, which DiscriminantTagsRegistry does not list: "+
			"the cache keys on the registered tags alone, so every value of those labels lands on "+
			"one slot and all but the last is lost on the PRTG, Nagios and Web UI pull sinks. "+
			"Register them in http_cache.go, with the reason beside them.", def.ProbeName, missing)
	}
}

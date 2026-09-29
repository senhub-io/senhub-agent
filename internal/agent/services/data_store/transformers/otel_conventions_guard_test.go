package transformers

import (
	"strings"
	"testing"
)

// The otlp_receiver definition describes what other programs send, not
// what the agent collects. That makes it the one definition that must
// not transform anything: a relayed datapoint reaches every output
// through the mapper's ingest short-circuit, so a name, a unit or a
// scale written here would describe a series nobody sends, and the
// items built from it would stay empty for ever.
func TestTheRelayDefinitionRenamesAndRescalesNothing(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	def, ok := defs["otlp_receiver"]
	if !ok {
		t.Fatal("no otlp_receiver definition")
	}

	for _, m := range def.Metrics {
		if m.Otel == nil {
			t.Errorf("%s has no otel block; a relayed metric is named by its convention", m.Name)
			continue
		}
		if m.Otel.Name != m.Name {
			t.Errorf("%s is declared under the OTel name %q: the key would name a series that arrives under another name",
				m.Name, m.Otel.Name)
		}
		if m.Otel.ValueScale != 0 && m.Otel.ValueScale != 1 {
			t.Errorf("%s carries value_scale %v: the value is the application's, in the unit it chose",
				m.Name, m.Otel.ValueScale)
		}
		if m.Otel.Expand != nil {
			t.Errorf("%s expands into several series; a relayed metric is passed through as it arrives", m.Name)
		}
		if len(m.Otel.Attributes) > 0 {
			t.Errorf("%s adds static attributes %v; nothing here may add to what the sender said", m.Name, m.Otel.Attributes)
		}
	}
}

// Every metric splits on the sender first. Without it two applications
// reporting one convention land on the same series, which is the defect
// the emitter identity fixed in the Zabbix key.
func TestEveryRelayedConventionSplitsOnTheSenderFirst(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	def := defs["otlp_receiver"]
	for _, m := range def.Metrics {
		if len(m.MultiInstanceLabels) == 0 {
			t.Errorf("%s declares no dimensions; it would inherit the probe's and that is easy to read as deliberate", m.Name)
			continue
		}
		if m.MultiInstanceLabels[0] != "service.name" {
			t.Errorf("%s splits on %v: the sender comes first, or two applications share one series",
				m.Name, m.MultiInstanceLabels)
		}
	}
}

// The conventions are upstream names. A typo is invisible — the item is
// simply never fed — so the shape is checked rather than trusted.
func TestRelayedConventionNamesLookLikeConventions(t *testing.T) {
	defs, _ := Definitions()
	for _, m := range defs["otlp_receiver"].Metrics {
		if strings.HasPrefix(m.Name, "senhub.") {
			t.Errorf("%s is under our own namespace; this file carries other people's conventions", m.Name)
		}
		// A convention is dotted between its parts and may carry an
		// underscore inside one of them (active_requests,
		// recent_utilization).
		if strings.ContainsAny(m.Name, " \t") || !strings.Contains(m.Name, ".") {
			t.Errorf("%s is not a dotted OTel name", m.Name)
		}
	}
}

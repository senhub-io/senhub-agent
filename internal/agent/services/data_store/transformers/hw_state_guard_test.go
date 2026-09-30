package transformers

import (
	"testing"
)

// hw.status carries one series per hw.state, and the semantic conventions
// enumerate those states (model/hardware/registry.yaml). A value outside
// the list, such as "unknown", is a series no conforming backend reads,
// and a synthetic "unknown" from an unmatched raw value is one too: an
// expansion over hw.state must be closed, so an unreadable state leaves
// every series at 0.
func TestHWStatusUsesTheConventionStates(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	states := map[string]bool{"ok": true, "degraded": true, "failed": true, "needs_cleaning": true, "predicted_failure": true}
	for probe, def := range defs {
		for _, m := range def.Metrics {
			if m.Otel == nil || m.Otel.Skip || m.Otel.Name != "hw.status" {
				continue
			}
			static, hasStatic := m.Otel.Attributes["hw.state"]
			expand := m.Otel.Expand
			switch {
			case expand != nil && expand.Attribute == "hw.state":
				if hasStatic {
					t.Errorf("%s/%s: hw.state is both static and expanded", probe, m.Name)
				}
				if !expand.Closed {
					t.Errorf("%s/%s: the hw.state expansion must be closed, or an unmatched value emits hw.state=unknown", probe, m.Name)
				}
				for state := range expand.Mapping {
					if !states[state] {
						t.Errorf("%s/%s: hw.state %q is not a convention state", probe, m.Name, state)
					}
				}
			case hasStatic:
				if !states[static] {
					t.Errorf("%s/%s: hw.state %q is not a convention state", probe, m.Name, static)
				}
			default:
				t.Errorf("%s/%s: hw.status without hw.state, which the convention requires", probe, m.Name)
			}
		}
	}
}

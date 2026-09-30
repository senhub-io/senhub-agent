package transformers

import (
	"strings"
	"testing"
)

// The names the agent derives for Prometheus pass promtool's naming lint,
// except `_count` on an OTel `.count` gauge, which is the OpenTelemetry
// convention for counting objects and is kept on purpose (#978):
//   - an OTel unit is a base unit: seconds, not ms/us/ns/h/d; bytes per
//     second, not bits (an alert written in seconds was off by 1000);
//   - `.total` names a counter only; a gauge that counts objects is
//     `<object>.count`, a capacity `.limit`, a size `.size`.
//
// The relay definition (otlp_receiver) is exempt: it describes names
// other programs send, which the agent passes through untouched.
func TestOTelNamesFollowPrometheusNamingRules(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	nonBase := map[string]string{"ms": "s", "us": "s", "μs": "s", "ns": "s", "h": "s", "d": "s", "min": "s", "bit/s": "By/s", "KiBy": "By", "MiBy": "By", "GiBy": "By"}
	for probe, def := range defs {
		if probe == "otlp_receiver" {
			continue
		}
		for _, m := range def.Metrics {
			if m.Otel == nil || m.Otel.Skip {
				continue
			}
			if base, bad := nonBase[m.Otel.Unit]; bad {
				t.Errorf("%s/%s: OTel unit %q is not a base unit; use %q (the display unit can stay as the probe reports it)", probe, m.Name, m.Otel.Unit, base)
			}
			if strings.HasSuffix(m.Otel.Name, ".total") && m.Otel.Type != "counter" {
				t.Errorf("%s/%s: %q is a %s; .total is for counters (Prometheus reserves _total), use .count, .limit or .size", probe, m.Name, m.Otel.Name, m.Otel.Type)
			}
		}
	}
}

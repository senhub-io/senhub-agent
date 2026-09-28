package transformers

import (
	"io/fs"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// notProbeDefinitions are embedded beside the probe definitions but have
// another shape.
var notProbeDefinitions = map[string]bool{"nagios.yaml": true, "lookups.yaml": true}

// A key the definition schema does not define is a mistake every time,
// and ignoring it produces wrong data rather than a broken build: an
// `attributes:` written beside `otel:` instead of under it published
// three distinct Container Apps job states as one series, and a
// `customunit: "h"` that nothing reads left a Citrix duration in hours
// labelled as seconds. Each definition is decoded strictly here.
func TestDefinitionsHoldOnlyKeysTheSchemaDefines(t *testing.T) {
	entries, err := fs.ReadDir(definitionFiles, "definitions")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || notProbeDefinitions[e.Name()] {
			continue
		}
		raw, err := definitionFiles.ReadFile("definitions/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var def ProbeDefinition
		if err := yaml.UnmarshalStrict(raw, &def); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}
}

// convertibleTo is what the mapper knows how to bring to an OTel unit:
// the source units convertValue scales, plus the unit itself.
var convertibleTo = map[string][]string{
	"s":  {"s", "ms", "millisecond", "milliseconds", "us", "μs", "microsecond", "microseconds", "ns", "nanosecond", "nanoseconds", "h", "hour", "hours", "d", "day", "days"},
	"By": {"bytes", "by", "b", "kb", "kib", "kilobyte", "kibibyte", "mb", "mib", "megabyte", "mebibyte", "gb", "gib", "gigabyte", "gibibyte"},
}

// A metric published in seconds or bytes must come from a unit the
// mapper can convert, or the value leaves unscaled under a unit that
// says otherwise: a Citrix grace period of 48 hours was exported as 48
// seconds. A value_scale states the conversion explicitly and is exempt.
func TestATimeOrSizeMetricComesFromAUnitTheMapperConverts(t *testing.T) {
	defs, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for probe, d := range defs {
		for _, m := range d.Metrics {
			if m.Otel == nil || m.Otel.Skip || m.Otel.ValueScale != 0 {
				continue
			}
			allowed, checked := convertibleTo[m.Otel.Unit]
			if !checked {
				continue
			}
			ok := false
			for _, u := range allowed {
				if strings.EqualFold(m.Unit, u) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s.%s: unit %q cannot be converted to %q; the value would leave unscaled", probe, m.Name, m.Unit, m.Otel.Unit)
			}
		}
	}
}

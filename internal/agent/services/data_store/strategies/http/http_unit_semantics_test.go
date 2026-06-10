package http

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
)

// absoluteVolumeUnits are PRTG units that present a value as an
// absolute quantity. A per-second metric rendered with one of these
// displays a speed as a volume — the #314 (a) defect class.
var absoluteVolumeUnits = map[string]bool{
	"BytesMemory":    true,
	"BytesDisk":      true,
	"BytesBandwidth": true,
	"BytesFile":      true,
	"Count":          true,
}

// TestPRTGUnitSemantics_RatesNeverRenderAsAbsolutes walks every metric
// of every embedded probe definition whose otel.unit declares a
// per-second rate and asserts the derived PRTG unit is rate-appropriate
// (a Speed* native unit, or Custom with a "/s" suffix) — never an
// absolute volume/count (#314 a).
func TestPRTGUnitSemantics_RatesNeverRenderAsAbsolutes(t *testing.T) {
	converter, defs := newSemanticsHarness(t)

	checked := 0
	for probeName, def := range defs {
		for _, m := range def {
			if m.Otel == nil || !strings.HasSuffix(m.Otel.Unit, "/s") {
				continue
			}
			channel := buildChannelFor(converter, probeName, m.Name, m.Unit)
			if channel == nil {
				t.Errorf("%s/%s: nil channel", probeName, m.Name)
				continue
			}
			checked++
			switch {
			case strings.HasPrefix(channel.Unit, "Speed"):
				if channel.SpeedSize == "" || channel.SpeedTime != "Second" {
					t.Errorf("%s/%s (otel.unit %s): Speed unit without input scale (speedsize=%q speedtime=%q)",
						probeName, m.Name, m.Otel.Unit, channel.SpeedSize, channel.SpeedTime)
				}
			case channel.Unit == "Custom" && strings.HasSuffix(channel.CustomUnit, "/s"):
				// acceptable rate rendering
			default:
				t.Errorf("%s/%s (otel.unit %s): rate rendered as %q/%q — a speed displayed as an absolute",
					probeName, m.Name, m.Otel.Unit, channel.Unit, channel.CustomUnit)
			}
			if absoluteVolumeUnits[channel.Unit] {
				t.Errorf("%s/%s (otel.unit %s): rate rendered with absolute unit %s",
					probeName, m.Name, m.Otel.Unit, channel.Unit)
			}
		}
	}
	if checked < 40 {
		t.Errorf("only %d rate metrics checked — expected the ~45 the audit counted; definitions enumeration broken?", checked)
	}
}

// TestPRTGUnitSemantics_ByteContext asserts byte metrics map to a
// context-appropriate PRTG byte unit: disk/filesystem bytes are not
// displayed as memory, network bytes are not displayed as memory
// (#314 b).
func TestPRTGUnitSemantics_ByteContext(t *testing.T) {
	converter, defs := newSemanticsHarness(t)

	byteDisplayUnits := map[string]bool{"Bytes": true, "bytes": true, "B": true, "By": true}
	checked := 0
	for probeName, def := range defs {
		for _, m := range def {
			if !byteDisplayUnits[m.Unit] {
				continue
			}
			if m.Otel != nil && strings.HasSuffix(m.Otel.Unit, "/s") {
				continue // rates covered by the rate test
			}
			channel := buildChannelFor(converter, probeName, m.Name, m.Unit)
			if channel == nil {
				continue
			}
			checked++
			if !strings.HasPrefix(channel.Unit, "Bytes") {
				t.Errorf("%s/%s: byte metric rendered as %q", probeName, m.Name, channel.Unit)
				continue
			}
			otelName := ""
			if m.Otel != nil {
				otelName = strings.ToLower(m.Otel.Name)
			}
			switch {
			case strings.Contains(otelName, "memory") || strings.Contains(otelName, "swap"):
				if channel.Unit != "BytesMemory" {
					t.Errorf("%s/%s (otel %s): memory bytes rendered as %s", probeName, m.Name, otelName, channel.Unit)
				}
			case strings.Contains(otelName, "disk") || strings.Contains(otelName, "filesystem"):
				if channel.Unit != "BytesDisk" {
					t.Errorf("%s/%s (otel %s): disk bytes rendered as %s", probeName, m.Name, otelName, channel.Unit)
				}
			case strings.Contains(otelName, "network"):
				if channel.Unit != "BytesBandwidth" {
					t.Errorf("%s/%s (otel %s): network bytes rendered as %s", probeName, m.Name, otelName, channel.Unit)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no byte metrics checked — definitions enumeration broken?")
	}
}

// newSemanticsHarness builds a FormatConverter over the real embedded
// definitions plus the per-probe metric definitions to walk.
func newSemanticsHarness(t *testing.T) (*FormatConverter, map[string][]transformers.MetricDefinition) {
	t.Helper()
	args := &cliArgs.ParsedArgs{Env: "test", Verbose: false}
	registry := transformers.NewTransformerRegistry(logger.NewLogger(args))
	converter := NewFormatConverter(registry, newTestLogger(), nil)

	defs, err := transformers.DefinitionMetrics()
	if err != nil {
		t.Fatalf("DefinitionMetrics: %v", err)
	}
	return converter, defs
}

// buildChannelFor renders one metric the way the PRTG endpoint does.
func buildChannelFor(converter *FormatConverter, probeName, metricName, displayUnit string) *PRTGChannel {
	metric := CachedMetric{
		Value:      float64(1),
		Unit:       displayUnit,
		MetricName: metricName,
		ProbeName:  probeName,
		Tags:       map[string]string{"probe_type": probeName},
	}
	return converter.transformToPRTGChannel(metricName, metric)
}

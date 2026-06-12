package data_store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

const fallbackWarnMarker = "Probe has no transformer definition"

// newFallbackTestStore builds a dataStore whose log output is captured in
// buf, with a real transformer registry (embedded YAML definitions).
func newFallbackTestStore(buf *bytes.Buffer) *dataStore {
	zl := zerolog.New(buf)
	ds := &dataStore{
		logger:              logger.NewModuleLogger(&zl, "data_store"),
		transformerRegistry: transformers.NewTransformerRegistry(&zl),
	}
	empty := make([]SyncStrategy, 0)
	ds.strategies.Store(&empty)
	return ds
}

func makeDatapoint(probeType string, extraTags ...tags.Tag) datapoint.DataPoint {
	dpTags := append([]tags.Tag{
		{Key: "probe_name", Value: "My " + probeType + " Probe"},
		{Key: "probe_type", Value: probeType},
	}, extraTags...)
	return datapoint.DataPoint{
		Name:  "some.metric",
		Value: 42,
		Tags:  dpTags,
	}
}

func TestApplyUnitCorrections_FallbackCountsEveryDatapointWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	ds := newFallbackTestStore(&buf)
	dp := makeDatapoint("probe_type_without_yaml_276")

	before := agentstate.GetTransformerFallbacksTotal()
	for i := 0; i < 5; i++ {
		out := ds.applyUnitCorrections([]datapoint.DataPoint{dp})
		if len(out) != 1 {
			t.Fatalf("expected 1 datapoint out, got %d", len(out))
		}
		if out[0].Value != dp.Value {
			t.Fatalf("fallback must not change the value: got %v, want %v", out[0].Value, dp.Value)
		}
	}

	if got := agentstate.GetTransformerFallbacksTotal() - before; got != 5 {
		t.Errorf("transformer fallback counter: got +%d, want +5 (one per datapoint)", got)
	}
	if warns := strings.Count(buf.String(), fallbackWarnMarker); warns != 1 {
		t.Errorf("fallback warning fired %d times, want exactly 1\nlog output:\n%s", warns, buf.String())
	}
	if !strings.Contains(buf.String(), `"level":"warn"`) {
		t.Errorf("fallback message must be logged at Warn level\nlog output:\n%s", buf.String())
	}
}

func TestApplyUnitCorrections_FallbackWarnsPerProbeType(t *testing.T) {
	var buf bytes.Buffer
	ds := newFallbackTestStore(&buf)

	ds.applyUnitCorrections([]datapoint.DataPoint{
		makeDatapoint("undefined_probe_a"),
		makeDatapoint("undefined_probe_b"),
		makeDatapoint("undefined_probe_a"),
	})

	if warns := strings.Count(buf.String(), fallbackWarnMarker); warns != 2 {
		t.Errorf("expected 1 warning per distinct probe type (2 total), got %d\nlog output:\n%s", warns, buf.String())
	}
}

func TestApplyUnitCorrections_NoFallbackForDefinedProbe(t *testing.T) {
	var buf bytes.Buffer
	ds := newFallbackTestStore(&buf)
	before := agentstate.GetTransformerFallbacksTotal()

	ds.applyUnitCorrections([]datapoint.DataPoint{makeDatapoint("cpu")})

	if got := agentstate.GetTransformerFallbacksTotal() - before; got != 0 {
		t.Errorf("counter must not move for a probe with a YAML definition: got +%d", got)
	}
	if strings.Contains(buf.String(), fallbackWarnMarker) {
		t.Errorf("no fallback warning expected for a defined probe\nlog output:\n%s", buf.String())
	}
}

func TestApplyUnitCorrections_FallbackExemptsPassthroughDatapoints(t *testing.T) {
	var buf bytes.Buffer
	ds := newFallbackTestStore(&buf)
	before := agentstate.GetTransformerFallbacksTotal()

	ds.applyUnitCorrections([]datapoint.DataPoint{
		makeDatapoint("otlp_receiver_276", tags.Tag{Key: "metric_type", Value: "otlp_ingest"}),
		makeDatapoint("typed_passthrough_276", tags.Tag{Key: "otel_type", Value: "gauge"}),
	})

	if got := agentstate.GetTransformerFallbacksTotal() - before; got != 0 {
		t.Errorf("counter must not move for pass-through datapoints: got +%d", got)
	}
	if strings.Contains(buf.String(), fallbackWarnMarker) {
		t.Errorf("no fallback warning expected for pass-through datapoints\nlog output:\n%s", buf.String())
	}
}

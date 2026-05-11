package prometheus

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/agentmetrics"
)

// TestBuildAgentRecords_SerializesToValidPrometheus validates the
// integration between the neutral agent-metrics builder and the
// Prometheus serializer — the latter must accept and produce a
// well-formed text exposition for every record the former emits.
//
// This sits in the prometheus package (not in agentmetrics) because
// the serializer is Prometheus-specific. The test in
// internal/agent/services/data_store/agentmetrics/ covers the
// neutral builder shape; this one covers the end-to-end wire format.
func TestBuildAgentRecords_SerializesToValidPrometheus(t *testing.T) {
	snap := agentmetrics.AgentMetricsSnapshot{
		StartTime:    time.Now().Add(-1 * time.Minute),
		CacheEntries: 12,
		ProbesActive: 2,
		BuildVersion: "0.1.88",
		BuildCommit:  "abc1234",
	}
	recs := agentmetrics.BuildAgentRecords(snap)

	var buf bytes.Buffer
	if err := SerializeToTextExposition(recs, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	t.Logf("Output:\n%s", body)

	p := newTextParser()
	parsed, err := p.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("expfmt parse err: %v\nbody:\n%s", err, body)
	}
	for _, want := range []string{
		"senhub_agent_uptime_seconds",
		"senhub_agent_cache_entries",
		"senhub_agent_probes_active",
		"senhub_agent_probes_total",
		"senhub_agent_probes_healthy",
		"senhub_agent_collect_errors_total",
		"senhub_agent_build_info",
	} {
		if _, ok := parsed[want]; !ok {
			names := make([]string, 0, len(parsed))
			for n := range parsed {
				names = append(names, n)
			}
			t.Errorf("missing metric %q in parsed output, got %v", want, names)
		}
	}
}

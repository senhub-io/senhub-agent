package prometheus

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBuildAgentRecords_AlwaysIncludesCoreMetrics(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:    time.Now().Add(-5 * time.Minute),
		CacheEntries: 42,
		ProbesActive: 3,
	}
	recs := BuildAgentRecords(snap)
	// Must include uptime, cache.entries, probes.active. No build info because empty.
	if len(recs) != 6 {
		t.Fatalf("expected 6 records (no build info, no http requests), got %d", len(recs))
	}

	names := map[string]bool{}
	for _, r := range recs {
		names[r.Name] = true
	}
	for _, want := range []string{
		"senhub.agent.uptime_seconds",
		"senhub.agent.cache.entries",
		"senhub.agent.probes.active",
		"senhub.agent.probes.total",
		"senhub.agent.probes.healthy",
		"senhub.agent.collect.errors",
	} {
		if !names[want] {
			t.Errorf("missing record: %q", want)
		}
	}

	// Uptime should be ~300s give or take a few ms.
	for _, r := range recs {
		if r.Name == "senhub.agent.uptime_seconds" {
			if r.Value < 299 || r.Value > 305 {
				t.Errorf("uptime out of expected band: got %v", r.Value)
			}
		}
		if r.Name == "senhub.agent.cache.entries" && r.Value != 42 {
			t.Errorf("cache.entries: got %v, want 42", r.Value)
		}
		if r.Name == "senhub.agent.probes.active" && r.Value != 3 {
			t.Errorf("probes.active: got %v, want 3", r.Value)
		}
	}
}

func TestBuildAgentRecords_BuildInfoEmittedWhenSet(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:    time.Now(),
		BuildVersion: "0.1.88-beta",
		BuildCommit:  "abc1234",
	}
	recs := BuildAgentRecords(snap)
	var buildRec *OtelRecord
	for i := range recs {
		if recs[i].Name == "senhub.agent.build_info" {
			buildRec = &recs[i]
			break
		}
	}
	if buildRec == nil {
		t.Fatal("expected senhub.agent.build_info record")
	}
	if buildRec.Value != 1 {
		t.Errorf("build.info value: got %v, want 1", buildRec.Value)
	}
	if buildRec.Attributes["version"] != "0.1.88-beta" {
		t.Errorf("version label: got %q, want 0.1.88-beta", buildRec.Attributes["version"])
	}
	if buildRec.Attributes["commit"] != "abc1234" {
		t.Errorf("commit label: got %q, want abc1234", buildRec.Attributes["commit"])
	}
}

func TestBuildAgentRecords_OmitBuildInfoWhenEmpty(t *testing.T) {
	snap := AgentMetricsSnapshot{StartTime: time.Now()}
	recs := BuildAgentRecords(snap)
	for _, r := range recs {
		if r.Name == "senhub.agent.build_info" {
			t.Errorf("build.info should not be emitted when version+commit are empty")
		}
	}
}

func TestBuildAgentRecords_SerializesToValidPrometheus(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:    time.Now().Add(-1 * time.Minute),
		CacheEntries: 12,
		ProbesActive: 2,
		BuildVersion: "0.1.88",
		BuildCommit:  "abc1234",
	}
	recs := BuildAgentRecords(snap)

	var buf bytes.Buffer
	if err := SerializeToTextExposition(recs, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	t.Logf("Output:\n%s", body)

	// Round-trip through the canonical Prometheus parser.
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
			t.Errorf("missing metric %q in parsed output (got %v)", want, keys(parsed))
		}
	}
}

func TestBuildAgentRecords_HTTPRequestsPerEndpoint(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:    time.Now(),
		ProbesTotal:  5,
		ProbesHealthy: 4,
		CollectErrorsTotal: 17,
		HTTPRequestsByEndpoint: map[string]uint64{
			"/api/{agentkey}/prtg/metrics":    123,
			"/api/{agentkey}/prometheus/metrics": 45,
			"/health":                          789,
		},
	}
	recs := BuildAgentRecords(snap)

	httpRecs := 0
	endpointsSeen := map[string]float64{}
	for _, r := range recs {
		if r.Name == "senhub.agent.http.requests" {
			httpRecs++
			endpointsSeen[r.Attributes["endpoint"]] = r.Value
		}
	}
	if httpRecs != 3 {
		t.Errorf("expected 3 HTTP request records, got %d", httpRecs)
	}
	if endpointsSeen["/health"] != 789 {
		t.Errorf("/health count: got %v, want 789", endpointsSeen["/health"])
	}

	// Also verify probes.healthy / collect.errors values
	for _, r := range recs {
		if r.Name == "senhub.agent.probes.healthy" && r.Value != 4 {
			t.Errorf("probes.healthy: got %v, want 4", r.Value)
		}
		if r.Name == "senhub.agent.collect.errors" && r.Value != 17 {
			t.Errorf("collect.errors: got %v, want 17", r.Value)
		}
	}
}

func TestBuildAgentRecords_NoHTTPRecordsWhenMapEmpty(t *testing.T) {
	snap := AgentMetricsSnapshot{StartTime: time.Now()}
	recs := BuildAgentRecords(snap)
	for _, r := range recs {
		if r.Name == "senhub.agent.http.requests" {
			t.Errorf("expected no http.requests records when map is empty/nil")
		}
	}
}

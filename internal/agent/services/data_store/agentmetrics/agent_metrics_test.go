package agentmetrics

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

func TestBuildAgentRecords_AlwaysIncludesCoreMetrics(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:    time.Now().Add(-5 * time.Minute),
		CacheEntries: 42,
		ProbesActive: 3,
	}
	recs := BuildAgentRecords(snap)
	// 31 records when neither build info nor http_requests is set and no
	// OTLP drops / checkpoint errors have occurred yet:
	//   7 core         (uptime, cache.entries, probes.{active,total,healthy},
	//                   collect.errors, transformer.fallback)
	//   8 OTLP push    (metrics.pushed, logs.pushed, export.errors,
	//                   dropped_log_records, buffer.fill_ratio,
	//                   store_size, export.duration{window=last},
	//                   export.duration{window=mean})
	//   3 OTLP checkpoint (size, last_save_age, restored_entries)
	//   1 OTLP parallel  (sub_batches)
	//   4 OTLP logs queue (queue.records, queue.bytes, logs.queued,
	//                   logs.replayed)
	//   2 OTLP failover (active_endpoint_index, endpoint_switches)
	//   6 process      (cpu.time, memory.{resident,heap}, goroutines,
	//                   gc.cycles, open_fds)
	//
	// Note: `senhub.agent.otlp.dropped{reason=...}` and
	// `senhub.agent.otlp.checkpoint.errors{stage=...}` are emitted only
	// when their counter has been touched, so they don't count here.
	if len(recs) != 31 {
		t.Fatalf("expected 31 records (no build info, no http requests, no OTLP drops, no checkpoint errors), got %d", len(recs))
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
		"senhub.agent.transformer.fallback",
		"senhub.agent.otlp.metrics.pushed",
		"senhub.agent.otlp.logs.pushed",
		"senhub.agent.otlp.export.errors",
		"senhub.agent.otlp.dropped_log_records",
		"senhub.agent.otlp.buffer.fill_ratio",
		"senhub.agent.otlp.store_size",
		"senhub.agent.otlp.export.duration",
		"senhub.agent.otlp.checkpoint.size",
		"senhub.agent.otlp.checkpoint.last_save_age",
		"senhub.agent.otlp.checkpoint.restored_entries",
		"senhub.agent.otlp.parallel.sub_batches",
		"senhub.agent.process.cpu.time",
		"senhub.agent.process.memory.resident",
		"senhub.agent.process.memory.heap",
		"senhub.agent.process.goroutines",
		"senhub.agent.process.gc.cycles",
		"senhub.agent.process.open_fds",
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
	var buildRec *otelmapper.OtelRecord
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

func TestBuildAgentRecords_HTTPRequestsPerEndpoint(t *testing.T) {
	snap := AgentMetricsSnapshot{
		StartTime:          time.Now(),
		ProbesTotal:        5,
		ProbesHealthy:      4,
		CollectErrorsTotal: 17,
		HTTPRequestsByEndpoint: map[string]uint64{
			"/api/{agentkey}/prtg/metrics":       123,
			"/api/{agentkey}/prometheus/metrics": 45,
			"/health":                            789,
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

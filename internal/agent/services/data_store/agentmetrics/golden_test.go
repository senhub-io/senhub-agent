package agentmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

const goldenPath = "testdata/build_agent_records.golden"

var volatileRecords = map[string]bool{
	"senhub.agent.uptime_seconds":                   true,
	"senhub.agent.otlp.checkpoint.last_save_age":    true,
	"senhub.agent.process.cpu.time":                 true,
	"senhub.agent.process.memory.resident":          true,
	"senhub.agent.process.memory.heap":              true,
	"senhub.agent.process.goroutines":               true,
	"senhub.agent.process.gc.cycles":                true,
	"senhub.agent.process.open_fds":                 true,
	"senhub.agent.self_logs.published":              true,
	"senhub.agent.self_logs.dropped":                true,
	"senhub.agent.entity.attribute.conflicts":       true,
	"senhub.agent.entity.duplicates.merged":         true,
	"otel.sdk.processor.log.processed":              true,
	"otel.sdk.processor.log.queue.size":             true,
	"otel.sdk.processor.log.queue.capacity":         true,
	"senhub.agent.otlp.dropped_log_records":         true,
	"senhub.agent.otlp.dropped_span_batches":        true,
	"senhub.agent.otlp.buffer.fill_ratio":           true,
	"senhub.agent.transformer.fallback":             true,
	"senhub.agent.otlp.spans.relayed":               true,
	"senhub.agent.otlp.logs.relayed":                true,
	"senhub.agent.otlp.metrics.relayed":             true,
	"senhub.agent.otlp.checkpoint.restored_entries": true,
	"senhub.agent.otlp.parallel.sub_batches":        true,
	"senhub.agent.otlp.logs.queue.records":          true,
	"senhub.agent.otlp.logs.queue.bytes":            true,
	"senhub.agent.otlp.logs.queued":                 true,
	"senhub.agent.otlp.logs.replayed":               true,
	"senhub.agent.otlp.active_endpoint_index":       true,
	"senhub.agent.otlp.endpoint_switches":           true,
	"senhub.agent.otlp.checkpoint.size":             true,
}

func renderRecord(r otelmapper.OtelRecord) string {
	keys := make([]string, 0, len(r.Attributes))
	for k := range r.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]string, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, k+"="+r.Attributes[k])
	}
	value := fmt.Sprintf("%g", r.Value)
	if volatileRecords[r.Name] {
		value = "<volatile>"
	}
	return fmt.Sprintf("%s | unit=%q | type=%s | attrs={%s} | value=%s | desc=%q",
		r.Name, r.Unit, r.Type, strings.Join(attrs, ","), value, r.Description)
}

// renderRecords keeps the order of groups of records and sorts inside a
// run of the same metric name, where map iteration order is random.
func renderRecords(recs []otelmapper.OtelRecord) string {
	var out []string
	for i := 0; i < len(recs); {
		j := i
		var run []string
		for j < len(recs) && recs[j].Name == recs[i].Name {
			run = append(run, renderRecord(recs[j]))
			j++
		}
		sort.Strings(run)
		out = append(out, run...)
		i = j
	}
	return strings.Join(out, "\n") + "\n"
}

func resetAllAgentState(t *testing.T) {
	t.Helper()
	agentstate.ResetCollectErrorsForTest()
	agentstate.ResetExportSendFailedForTest()
	agentstate.ResetExportRejectedForTest()
	agentstate.ResetHTTPCacheDroppedForTest()
	agentstate.ResetLicenseInvalidForTest()
	agentstate.ResetOTLPExportErrorsBySignalForTest()
	agentstate.ResetOTLPCountersForTest()
	agentstate.ResetOTLPReceiverCountersForTest()
	agentstate.ResetOTLPReceiverCoverageForTest()
	agentstate.ResetPushBufferDroppedForTest()
	agentstate.ResetStrategyFailuresForTest()
	agentstate.ResetUpdateRejectedForTest()
	agentstate.ClearConfigWatchDisabled()
}

func TestBuildAgentRecords_GoldenOutput(t *testing.T) {
	resetAllAgentState(t)
	t.Cleanup(func() { resetAllAgentState(t) })

	agentstate.IncrementCollectErrors("redfish", "timeout")
	agentstate.IncrementCollectErrors("redfish", "timeout")
	agentstate.IncrementOTLPMetricsPushed(11)
	agentstate.IncrementOTLPLogsPushed()
	agentstate.IncrementOTLPExportErrors("logs")
	agentstate.IncrementOTLPDropped("store_cap")
	agentstate.RecordOTLPStoreSize(77)
	agentstate.RecordOTLPExportDuration(3 * time.Second)
	agentstate.IncrementOTLPCheckpointErrors("rename")
	agentstate.RecordOTLPCheckpointSize(2048)
	agentstate.RecordStrategyFailure("otlp", agentstate.StrategyFailureStart, "boom")
	agentstate.RecordConfigWatchDisabled(agentstate.ConfigWatchNotWatched, "detail")
	agentstate.IncrementUpdateRejected("signature_invalid")
	agentstate.SetLicenseInvalid("binding_mismatch")
	agentstate.IncrementHTTPCacheDropped("http_cache_cap")
	agentstate.IncrementPushBufferDropped("senhub", 5)
	agentstate.IncrementExportSendFailed("otlp", "transport")
	agentstate.IncrementExportRejected("otlp", "metrics", 9)
	agentstate.IncrementOTLPReceiverIngested("logs", 4)
	agentstate.IncrementOTLPReceiverDropped("traces", "no_sink", 3)
	agentstate.RecordOTLPReceiverCoverage("logs", "uds", "svc-a", 6, false)

	snap := AgentMetricsSnapshot{
		StartTime:              time.Now().Add(-time.Minute),
		CacheEntries:           42,
		ProbesActive:           3,
		ProbesTotal:            5,
		ProbesHealthy:          4,
		HTTPRequestsByEndpoint: map[string]uint64{"/metrics": 8},
		BuildVersion:           "1.2.3",
		BuildCommit:            "abcdef0",
	}
	got := renderRecords(BuildAgentRecords(snap))

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("BuildAgentRecords output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

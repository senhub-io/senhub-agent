// Package http — tests for the /info/otlp endpoint.
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestHandleInfoOTLP_EmptyStateShape(t *testing.T) {
	agentstate.ResetOTLPCountersForTest()

	agentKey := "test-agent-key"
	apiManager := createTestAPIManager(newMockConfigWithLicense(agentKey, ""))

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/info/otlp", agentKey), nil)
	req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})
	w := httptest.NewRecorder()

	apiManager.HandleInfoOTLP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var got OTLPInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Pipeline counters all zero, but the maps must be non-nil so
	// frontends can iterate without a guard.
	if got.Pipeline.MetricsPushedTotal != 0 || got.Pipeline.DroppedTotal != 0 {
		t.Errorf("expected empty pipeline counters, got %+v", got.Pipeline)
	}
	if got.Pipeline.DroppedByReason == nil {
		t.Errorf("dropped_by_reason should be non-nil (empty map), got nil")
	}
	if got.Checkpoint.ErrorsByStage == nil {
		t.Errorf("checkpoint.errors_by_stage should be non-nil (empty map), got nil")
	}
	if got.Store.Size != 0 {
		t.Errorf("store.size: got %d, want 0", got.Store.Size)
	}
	if got.Parallel.SubBatches != 0 {
		t.Errorf("parallel.sub_batches: got %d, want 0", got.Parallel.SubBatches)
	}
}

func TestHandleInfoOTLP_ReflectsCounters(t *testing.T) {
	agentstate.ResetOTLPCountersForTest()

	// Seed every counter with a distinct value so we can verify each
	// field is wired to the right getter (not just any non-zero value).
	agentstate.IncrementOTLPMetricsPushed(1234)
	agentstate.IncrementOTLPLogsPushed()
	agentstate.IncrementOTLPLogsPushed()
	agentstate.IncrementOTLPLogsPushed() // 3
	agentstate.IncrementOTLPExportErrors()
	agentstate.IncrementOTLPDropped("store_cap")
	agentstate.IncrementOTLPDropped("store_cap")
	agentstate.IncrementOTLPDropped("memory_soft_limit") // 2 + 1 = 3 total
	agentstate.RecordOTLPStoreSize(4825)
	agentstate.RecordOTLPExportDuration(16670 * time.Millisecond)
	agentstate.RecordOTLPExportDuration(18210 * time.Millisecond) // mean = 17440 ms
	agentstate.RecordOTLPCheckpointSize(2048)
	agentstate.RecordOTLPCheckpointLastSave(time.Now().Add(-12 * time.Second).UnixNano())
	agentstate.RecordOTLPCheckpointRestored(42)
	agentstate.IncrementOTLPCheckpointErrors("fsync")
	agentstate.RecordOTLPSubBatchCount(6)

	agentKey := "test-agent-key"
	apiManager := createTestAPIManager(newMockConfigWithLicense(agentKey, ""))

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/info/otlp", agentKey), nil)
	req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})
	w := httptest.NewRecorder()

	apiManager.HandleInfoOTLP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var got OTLPInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Pipeline.MetricsPushedTotal != 1234 {
		t.Errorf("metrics_pushed_total: got %d, want 1234", got.Pipeline.MetricsPushedTotal)
	}
	if got.Pipeline.LogsPushedTotal != 3 {
		t.Errorf("logs_pushed_total: got %d, want 3", got.Pipeline.LogsPushedTotal)
	}
	if got.Pipeline.ExportErrorsTotal != 1 {
		t.Errorf("export_errors_total: got %d, want 1", got.Pipeline.ExportErrorsTotal)
	}
	if got.Pipeline.DroppedTotal != 3 {
		t.Errorf("dropped_total: got %d, want 3", got.Pipeline.DroppedTotal)
	}
	if got.Pipeline.DroppedByReason["store_cap"] != 2 {
		t.Errorf("dropped_by_reason[store_cap]: got %d, want 2",
			got.Pipeline.DroppedByReason["store_cap"])
	}
	if got.Pipeline.DroppedByReason["memory_soft_limit"] != 1 {
		t.Errorf("dropped_by_reason[memory_soft_limit]: got %d, want 1",
			got.Pipeline.DroppedByReason["memory_soft_limit"])
	}

	if got.Store.Size != 4825 {
		t.Errorf("store.size: got %d, want 4825", got.Store.Size)
	}

	if got.ExportDuration.LastMs != 18210 {
		t.Errorf("export_duration.last_ms: got %.1f, want 18210", got.ExportDuration.LastMs)
	}
	// Mean = (16670 + 18210) / 2 = 17440.
	if got.ExportDuration.MeanMs != 17440 {
		t.Errorf("export_duration.mean_ms: got %.1f, want 17440", got.ExportDuration.MeanMs)
	}

	if got.Checkpoint.SizeBytes != 2048 {
		t.Errorf("checkpoint.size_bytes: got %d, want 2048", got.Checkpoint.SizeBytes)
	}
	// Last save age is wall-clock-dependent; allow a generous band.
	if got.Checkpoint.LastSaveAgeSeconds < 10 || got.Checkpoint.LastSaveAgeSeconds > 30 {
		t.Errorf("checkpoint.last_save_age_seconds: got %.1f, want ~12", got.Checkpoint.LastSaveAgeSeconds)
	}
	if got.Checkpoint.RestoredEntries != 42 {
		t.Errorf("checkpoint.restored_entries: got %d, want 42", got.Checkpoint.RestoredEntries)
	}
	if got.Checkpoint.ErrorsTotal != 1 {
		t.Errorf("checkpoint.errors_total: got %d, want 1", got.Checkpoint.ErrorsTotal)
	}
	if got.Checkpoint.ErrorsByStage["fsync"] != 1 {
		t.Errorf("checkpoint.errors_by_stage[fsync]: got %d, want 1", got.Checkpoint.ErrorsByStage["fsync"])
	}

	if got.Parallel.SubBatches != 6 {
		t.Errorf("parallel.sub_batches: got %d, want 6", got.Parallel.SubBatches)
	}
}

func TestHandleInfoOTLP_UnauthorizedRejected(t *testing.T) {
	agentstate.ResetOTLPCountersForTest()

	agentKey := "test-agent-key"
	apiManager := createTestAPIManager(newMockConfigWithLicense(agentKey, ""))

	wrongKey := "wrong-key"
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/%s/info/otlp", wrongKey), nil)
	req = mux.SetURLVars(req, map[string]string{"agentkey": wrongKey})
	w := httptest.NewRecorder()

	apiManager.HandleInfoOTLP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

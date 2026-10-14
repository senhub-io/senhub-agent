package swarm

import (
	"net/http"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
)

func TestCollect_DatapointsCarryClusterIdentityAndCheckResult(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm":    map[string]any{"ID": "c1", "Spec": map[string]any{"Name": "prod"}},
		"/nodes":    []any{},
		"/services": []any{},
		"/tasks":    []any{},
		"/networks": []any{},
	}))

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	clusterID := map[string]string{"service.instance.id": "swarm://c1"}
	if v, ok := value(t, points, "senhub.swarm.up", clusterID); !ok || v != 1 {
		t.Error("up metric must carry the cluster service.instance.id")
	}
	if v, ok := value(t, points, "swarm.cluster.nodes", clusterID); !ok || v != 0 {
		t.Error("cluster node metric must carry the cluster service.instance.id")
	}
	if v, ok := value(t, points, dbcommon.CheckResultMetric, map[string]string{
		"service.instance.id": "swarm://c1",
		"metric_type":         "status",
	}); !ok || v != 0 {
		t.Error("check result must be 0 on a successful collection and carry the cluster identity")
	}
}

func TestCollect_PartialListFailureKeepsClusterIdentityAndClassifies(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm": map[string]any{"ID": "c1"},
		"/nodes": errBody{http.StatusInternalServerError, "rpc error: the swarm does not have a leader"},
	}))

	points, err := p.Collect()
	if err == nil {
		t.Fatal("a failed list must surface as an error, not be swallowed")
	}
	if up, _ := value(t, points, "senhub.swarm.up", nil); up != 1 {
		t.Error("the swarm itself answered, so up must stay 1 — the failure is per-endpoint")
	}
	if v, ok := value(t, points, dbcommon.CheckResultMetric, map[string]string{
		"service.instance.id": "swarm://c1",
		"metric_type":         "status",
	}); !ok || v != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result = %v, want target down", v)
	}
	if v, ok := value(t, points, "swarm.cluster.nodes", nil); ok {
		t.Errorf("swarm.cluster.nodes was emitted as %v after a failed read; an unreadable cluster must emit nothing rather than zero", v)
	}
}

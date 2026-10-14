package mongodb

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func requireIdentityTag(t *testing.T, dp data_store.DataPoint, wantID string) {
	t.Helper()
	for _, tag := range dp.Tags {
		if tag.Key == "db.instance.id" {
			if tag.Value != wantID {
				t.Errorf("datapoint %s has db.instance.id=%q, want %q", dp.Name, tag.Value, wantID)
			}
			return
		}
	}
	if wantID != "" {
		t.Errorf("datapoint %s missing db.instance.id tag", dp.Name)
	}
}

func TestCollect_Failure_IdentityAndCheckResult(t *testing.T) {
	p, err := NewMongoDBProbe(map[string]interface{}{
		"uri":      "mongodb://127.0.0.1:1",
		"timeout":  1,
		"interval": 30,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewMongoDBProbe: %v", err)
	}
	mp := p.(*mongoDBProbe)
	mp.SetName("mongo-failure")

	if err := mp.OnStart(nil); err != nil {
		t.Fatalf("OnStart returned err: %v", err)
	}

	pts, err := mp.Collect()
	if err != nil {
		t.Fatalf("Collect must not return error on unreachable MongoDB, got: %v", err)
	}

	// Never reached, so no entity exists yet: a key here would name nothing.
	wantID := ""

	upFound := false
	checkFound := false
	for _, dp := range pts {
		requireIdentityTag(t, dp, wantID)
		switch dp.Name {
		case "senhub.mongodb.up":
			upFound = true
			if dp.Value != 0 {
				t.Errorf("up = %v, want 0 when MongoDB unreachable", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			checkFound = true
			want := float64(dbcommon.CheckTargetDown)
			if dp.Value != want {
				t.Errorf("check result = %v, want %v", dp.Value, want)
			}
		}
	}
	if !upFound {
		t.Errorf("senhub.mongodb.up datapoint missing")
	}
	if !checkFound {
		t.Errorf("check result datapoint missing")
	}
}

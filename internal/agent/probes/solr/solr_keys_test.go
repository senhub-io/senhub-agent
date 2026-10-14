package solr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func solrTagValue(dp data_store.DataPoint, key string) (string, bool) {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func TestSolrKeys_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/solr/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metricsPayload())
	})
	mux.HandleFunc("/solr/admin/cores", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(coresPayload())
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	probe, err := NewSolrProbe(map[string]interface{}{
		"endpoint":      srv.URL,
		"instance_name": "test-solr-instance",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	p := probe.(*SolrProbe)
	p.SetName("solr-test")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	expectedID := p.entitySrc.currentID()
	if expectedID == "" {
		t.Fatalf("expected pinned entity id after successful collection")
	}

	checkCount := 0
	for _, dp := range points {
		id, ok := solrTagValue(dp, "db.instance.id")
		if !ok {
			t.Errorf("datapoint %q missing db.instance.id identity tag", dp.Name)
			continue
		}
		if id != expectedID {
			t.Errorf("datapoint %q db.instance.id = %q, want %q", dp.Name, id, expectedID)
		}

		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("check result value = %v, want %d", dp.Value, dbcommon.CheckOK)
			}
			if metricType, _ := solrTagValue(dp, "metric_type"); metricType != "status" {
				t.Errorf("check result metric_type = %q, want status", metricType)
			}
		}
	}

	if checkCount != 1 {
		t.Errorf("check result emitted %d times, want 1", checkCount)
	}
}

func TestSolrKeys_Down(t *testing.T) {
	probe, err := NewSolrProbe(map[string]interface{}{
		"endpoint":      "http://127.0.0.1:19876",
		"timeout":       1,
		"instance_name": "fallback-solr-instance",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	p := probe.(*SolrProbe)
	p.SetName("solr-down")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error, want nil: %v", err)
	}

	var up *data_store.DataPoint
	var check *data_store.DataPoint
	for _, dp := range points {
		dp := dp
		switch dp.Name {
		case "senhub.solr.up":
			up = &dp
		case dbcommon.CheckResultMetric:
			check = &dp
		}
	}

	if up == nil {
		t.Fatal("senhub.solr.up not emitted")
	}
	if up.Value != 0 {
		t.Errorf("senhub.solr.up = %v, want 0", up.Value)
	}
	if id, ok := solrTagValue(*up, "db.instance.id"); !ok || id != "fallback-solr-instance" {
		t.Errorf("up metric db.instance.id = %q, ok=%v, want fallback-solr-instance", id, ok)
	}

	if check == nil {
		t.Fatal("check result not emitted")
	}
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result value = %v, want %d", check.Value, dbcommon.CheckTargetDown)
	}
	if id, ok := solrTagValue(*check, "db.instance.id"); !ok || id != "fallback-solr-instance" {
		t.Errorf("check result db.instance.id = %q, ok=%v, want fallback-solr-instance", id, ok)
	}

	checkCount := 0
	for _, dp := range points {
		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
		}
	}
	if checkCount != 1 {
		t.Errorf("check result emitted %d times, want 1", checkCount)
	}
}

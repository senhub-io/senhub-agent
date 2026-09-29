package zabbix

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// A private tag routes the legacy PRTG push and never becomes an LLD
// macro or an item key part.
func TestStoreDropsPrivateTags(t *testing.T) {
	st := newStore()
	st.upsert(datapoint.DataPoint{
		Name: "http.duration", Value: 0.02, Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "site"}, {Key: "probe_type", Value: "load_webapp"},
			{Key: "url", Value: "https://example.com/"},
			{Key: "prtg_metric_id", Value: "https_example.com_[name]", Private: true},
		},
	})
	got, _ := st.snapshot(time.Now(), time.Minute)
	if len(got) != 1 {
		t.Fatalf("snapshot holds %d series, want 1", len(got))
	}
	if _, leaked := got[0].Tags["prtg_metric_id"]; leaked {
		t.Errorf("private tag reached the Zabbix store: %v", got[0].Tags)
	}
	if got[0].Tags["url"] == "" {
		t.Errorf("public tag lost: %v", got[0].Tags)
	}
}

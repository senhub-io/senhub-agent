package http

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/exporthook"
)

var registerSelfRecords sync.Once

func TestSelfRecordsEndpoint(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	url := "/api/" + testAdminKey + "/info/self-records"

	// Once a provider is registered, it stays for the process: the empty
	// case can only be checked on the first run.
	if len(exporthook.SelfRecords(time.Now())) == 0 {
		code, resp := doJSON(t, router, "GET", url, nil)
		recs, _ := resp["records"].([]interface{})
		if code != 200 || recs == nil || len(recs) != 0 {
			t.Fatalf("an empty registry must answer an empty list, got %d %v", code, resp)
		}
	}

	registerSelfRecords.Do(func() {
		exporthook.RegisterSelfRecordProvider(func(time.Time) []exporthook.SelfRecord {
			return []exporthook.SelfRecord{
				{Name: "b.metric", Type: "counter", Value: 3},
				{Name: "a.metric", Unit: "{series}", Type: "gauge", Description: "d", Value: 7, Attributes: map[string]string{"output": "otlp"}},
			}
		})
	})

	code, resp := doJSON(t, router, "GET", url, nil)
	recs, _ := resp["records"].([]interface{})
	if code != 200 || len(recs) != 2 {
		t.Fatalf("want both records, got %d %v", code, resp)
	}
	first := recs[0].(map[string]interface{})
	if first["name"] != "a.metric" || first["value"] != float64(7) || first["unit"] != "{series}" {
		t.Errorf("records must come sorted by name with their fields, got %v", first)
	}
	if first["attributes"].(map[string]interface{})["output"] != "otlp" {
		t.Errorf("attributes lost: %v", first)
	}
	if recs[1].(map[string]interface{})["attributes"] == nil {
		t.Error("a record without attributes must carry an empty object, not null")
	}
}

func TestSelfRecordsEndpointRefusesAWrongKey(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	if code, _ := doJSON(t, router, "GET", "/api/not-the-key/info/self-records", nil); code != http.StatusUnauthorized {
		t.Errorf("a wrong key must be refused like info/system, got %d", code)
	}
}

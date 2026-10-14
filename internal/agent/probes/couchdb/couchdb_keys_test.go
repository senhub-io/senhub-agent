package couchdb

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func tagValue(tagsSlice []tags.Tag, key string) string {
	for _, tag := range tagsSlice {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func TestCollect_KeysAndCheckResult_Success(t *testing.T) {
	stats := minimalStats()
	const serverUUID = "abc123def456"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_node/_local/_stats":
			if err := json.NewEncoder(w).Encode(stats); err != nil {
				t.Errorf("encoding stats: %v", err)
			}
		case "/":
			if err := json.NewEncoder(w).Encode(map[string]string{"uuid": serverUUID, "version": "3.3.2"}); err != nil {
				t.Errorf("encoding root: %v", err)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p, err := NewCouchDBProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	expectedID := "couchdb:" + serverUUID
	foundCheck := false
	foundUp := false

	for _, dp := range points {
		if got := tagValue(dp.Tags, "db.instance.id"); got != expectedID {
			t.Errorf("%s db.instance.id = %q, want %q", dp.Name, got, expectedID)
		}

		switch dp.Name {
		case "senhub.couchdb.up":
			foundUp = true
			if dp.Value != 1 {
				t.Errorf("senhub.couchdb.up = %v, want 1", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			foundCheck = true
			if dp.Value != 0 {
				t.Errorf("check result = %v, want 0", dp.Value)
			}
			if got := tagValue(dp.Tags, "metric_type"); got != "status" {
				t.Errorf("check result metric_type = %q, want status", got)
			}
		}
	}

	if !foundCheck {
		t.Error("check result datapoint missing")
	}
	if !foundUp {
		t.Error("senhub.couchdb.up datapoint missing")
	}
}

func TestCollect_KeysAndCheckResult_Unreachable(t *testing.T) {
	p, err := NewCouchDBProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:1",
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCouchDBProbe: %v", err)
	}

	cp := p.(*CouchDBProbe)
	cp.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error on unreachable endpoint, got: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints on unreachable endpoint")
	}

	// Never reached, so no entity exists yet: a key here would name nothing.
	expectedID := ""
	foundUp := false
	foundCheck := false

	for _, dp := range points {
		if got := tagValue(dp.Tags, "db.instance.id"); got != expectedID {
			t.Errorf("%s db.instance.id = %q, want %q", dp.Name, got, expectedID)
		}

		switch dp.Name {
		case "senhub.couchdb.up":
			foundUp = true
			if dp.Value != 0 {
				t.Errorf("senhub.couchdb.up = %v, want 0", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			foundCheck = true
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("check result = %v, want %v", dp.Value, float64(dbcommon.CheckTargetDown))
			}
			if got := tagValue(dp.Tags, "metric_type"); got != "status" {
				t.Errorf("check result metric_type = %q, want status", got)
			}
		}
	}

	if !foundUp {
		t.Error("senhub.couchdb.up datapoint missing")
	}
	if !foundCheck {
		t.Error("check result datapoint missing")
	}
}

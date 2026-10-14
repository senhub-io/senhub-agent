package influxdb

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func assertEntityID(t *testing.T, dp data_store.DataPoint, want string) {
	t.Helper()
	for _, tg := range dp.Tags {
		if tg.Key == "db.instance.id" {
			if tg.Value != want {
				t.Fatalf("%s db.instance.id = %q, want %q", dp.Name, tg.Value, want)
			}
			return
		}
	}
	t.Fatalf("%s missing db.instance.id tag", dp.Name)
}

func TestCollect_EntityKeysAndCheckResult_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"pass","version":"2.7.0"}`))
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("go_goroutines 55\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantID := p.entitySrc.InstanceID()
	foundCheck := false
	var checkValue float64
	for _, dp := range points {
		assertEntityID(t, dp, wantID)
		if dp.Name == dbcommon.CheckResultMetric {
			foundCheck = true
			checkValue = dp.Value
		}
	}
	if !foundCheck {
		t.Fatal("check result metric not found")
	}
	if checkValue != 0 {
		t.Errorf("check result = %v, want 0", checkValue)
	}
}

func TestCollect_EntityKeysAndCheckResult_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return error on unhealthy server: %v", err)
	}

	wantID := p.entitySrc.InstanceID()
	sawUp := false
	sawCheck := false
	for _, dp := range points {
		assertEntityID(t, dp, wantID)

		switch dp.Name {
		case "senhub.influxdb.up":
			sawUp = true
			if dp.Value != 0 {
				t.Errorf("senhub.influxdb.up = %v, want 0", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			sawCheck = true
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, dp.Value, float64(dbcommon.CheckTargetDown))
			}
		}
	}
	if !sawUp {
		t.Error("senhub.influxdb.up not found")
	}
	if !sawCheck {
		t.Error("check result metric not found")
	}
}

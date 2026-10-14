package unifi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(tags []tags.Tag, key string) string {
	for _, tg := range tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

func TestKeys_SuccessfulCollection(t *testing.T) {
	srv := stubController(t,
		map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"subsystem": "wan", "tx_bytes-r": 10.0, "rx_bytes-r": 20.0},
			},
		},
		map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{
					"type": "uap", "state": 1, "adopted": true,
					"name": "AP-1", "num_sta": 1.0, "satisfaction": 90.0,
					"system-stats": map[string]interface{}{"cpu": "10", "mem": "20"},
				},
			},
		},
		map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"is_wired": false},
			},
		},
	)
	defer srv.Close()

	p := newTestProbe(t, srv)
	p.entitySource.hostID = func() string { return "host-1" }

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no points")
	}

	wantID := "unifi@host-1"
	checkCount := 0
	for _, dp := range points {
		if got := tagValue(dp.Tags, idKeyServiceInstanceID); got != wantID {
			t.Errorf("%s: service.instance.id = %q; want %q", dp.Name, got, wantID)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != 0 {
				t.Errorf("%s value = %v; want 0", dp.Name, dp.Value)
			}
			if mt := tagValue(dp.Tags, "metric_type"); mt != "status" {
				t.Errorf("%s metric_type = %q; want status", dp.Name, mt)
			}
		}
	}
	if checkCount != 1 {
		t.Errorf("check result metrics = %d; want 1", checkCount)
	}
}

func TestKeys_LoginFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := newTestProbe(t, srv)
	p.entitySource.hostID = func() string { return "host-9" }

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantID := "unifi@host-9"
	wantCheck := float64(dbcommon.ClassifyHTTPStatus(http.StatusUnauthorized))
	upValue := -1.0
	checkCount := 0
	checkValue := -1.0
	for _, dp := range points {
		if got := tagValue(dp.Tags, idKeyServiceInstanceID); got != wantID {
			t.Errorf("%s: service.instance.id = %q; want %q", dp.Name, got, wantID)
		}
		switch dp.Name {
		case "senhub.unifi.up":
			upValue = dp.Value
		case dbcommon.CheckResultMetric:
			checkCount++
			checkValue = dp.Value
			if mt := tagValue(dp.Tags, "metric_type"); mt != "status" {
				t.Errorf("%s metric_type = %q; want status", dp.Name, mt)
			}
		}
	}

	if upValue != 0 {
		t.Errorf("senhub.unifi.up = %v; want 0", upValue)
	}
	if checkCount != 1 {
		t.Errorf("check result metrics = %d; want 1", checkCount)
	}
	if checkValue != wantCheck {
		t.Errorf("%s = %v; want %v", dbcommon.CheckResultMetric, checkValue, wantCheck)
	}
}

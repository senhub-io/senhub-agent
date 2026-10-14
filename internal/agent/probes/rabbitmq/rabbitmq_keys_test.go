package rabbitmq

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func tagValue(dp data_store.DataPoint, key string) string {
	for _, tag := range dp.Tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func TestCollect_EntityIdentityKeysOnAllPoints_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/overview":
			_, _ = w.Write([]byte(`{"node":"rabbit@myhost","message_stats":{"publish":10},"queue_totals":{"messages_ready":1},"object_totals":{"queues":1}}`))
		case "/api/nodes":
			_, _ = w.Write([]byte(`[{"name":"rabbit@myhost","mem_used":10,"running":true}]`))
		case "/api/queues":
			_, _ = w.Write([]byte(`[{"name":"q","vhost":"/","messages_ready":2}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	probe, err := NewRabbitMQProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() returned unexpected error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	const wantID = "rabbitmq:rabbit@myhost"
	checkCount := 0

	for _, dp := range points {
		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != 0 {
				t.Errorf("check result = %v, want 0", dp.Value)
			}
			if got := tagValue(dp, "service.instance.id"); got != wantID {
				t.Errorf("check result service.instance.id = %q, want %q", got, wantID)
			}
			if got := tagValue(dp, "metric_type"); got != "status" {
				t.Errorf("check result metric_type = %q, want %q", got, "status")
			}
			continue
		}

		if got := tagValue(dp, "service.instance.id"); got != wantID {
			t.Errorf("datapoint %q service.instance.id = %q, want %q", dp.Name, got, wantID)
		}
	}

	if checkCount != 1 {
		t.Errorf("check result datapoints = %d, want 1", checkCount)
	}
}

func TestCollect_EntityIdentityKeysOnFailedConnection(t *testing.T) {
	const wantID = "rabbitmq@fallback-host"

	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint":      "http://127.0.0.1:1",
		"instance_name": wantID,
		"timeout":       1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error on unreachable target, got: %v", err)
	}

	upFound := false
	checkCount := 0

	for _, dp := range points {
		if dp.Name == "senhub.rabbitmq.up" {
			upFound = true
			if dp.Value != 0 {
				t.Errorf("senhub.rabbitmq.up = %v, want 0", dp.Value)
			}
			if got := tagValue(dp, "service.instance.id"); got != wantID {
				t.Errorf("senhub.rabbitmq.up service.instance.id = %q, want %q", got, wantID)
			}
		}

		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("check result = %v, want %v", dp.Value, float64(dbcommon.CheckTargetDown))
			}
			if got := tagValue(dp, "service.instance.id"); got != wantID {
				t.Errorf("check result service.instance.id = %q, want %q", got, wantID)
			}
			if got := tagValue(dp, "metric_type"); got != "status" {
				t.Errorf("check result metric_type = %q, want %q", got, "status")
			}
		}
	}

	if !upFound {
		t.Error("senhub.rabbitmq.up not found in output")
	}
	if checkCount != 1 {
		t.Errorf("check result datapoints = %d, want 1", checkCount)
	}
}

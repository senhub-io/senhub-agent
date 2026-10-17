package activemq

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tg := range tags {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func TestCollect_DatapointsCarryServiceInstanceID(t *testing.T) {
	srv := jolokiaServer(t)
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"jolokia_url": srv.URL + "/api/jolokia",
		"username":    "",
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	const wantID = "activemq:test-broker-uuid-1234"
	checkCount := 0

	for _, dp := range points {
		id, ok := tagValue(dp.Tags, "service.instance.id")
		if !ok || id != wantID {
			t.Errorf("datapoint %s service.instance.id = %q (present=%t), want %q", dp.Name, id, ok, wantID)
		}

		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != 0 {
				t.Errorf("%s = %v, want 0", dbcommon.CheckResultMetric, dp.Value)
			}
			mt, ok := tagValue(dp.Tags, "metric_type")
			if !ok || mt != "status" {
				t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, mt)
			}
		}
	}

	if checkCount != 1 {
		t.Fatalf("expected exactly one %s datapoint, got %d", dbcommon.CheckResultMetric, checkCount)
	}
}

func TestCollect_BrokerDown_EmitsCheckResultAndFallbackIdentity(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"jolokia_url": "http://127.0.0.1:1",
		"timeout":     1,
	})
	p.entitySrc.hostIDFn = func() string { return "testhost" }

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error on broker failure: %v", err)
	}

	const wantID = "activemq@testhost"
	upFound := false
	checkCount := 0

	for _, dp := range points {
		if dp.Name == "senhub.activemq.up" {
			upFound = true
			if dp.Value != 0 {
				t.Errorf("senhub.activemq.up = %v, want 0 when broker is down", dp.Value)
			}
		}

		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, dp.Value, dbcommon.CheckTargetDown)
			}
			mt, ok := tagValue(dp.Tags, "metric_type")
			if !ok || mt != "status" {
				t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, mt)
			}
		}

		id, ok := tagValue(dp.Tags, "service.instance.id")
		if !ok || id != wantID {
			t.Errorf("datapoint %s service.instance.id = %q (present=%t), want %q", dp.Name, id, ok, wantID)
		}
	}

	if !upFound {
		t.Error("senhub.activemq.up datapoint missing")
	}
	if checkCount != 1 {
		t.Fatalf("expected exactly one %s datapoint, got %d", dbcommon.CheckResultMetric, checkCount)
	}
}

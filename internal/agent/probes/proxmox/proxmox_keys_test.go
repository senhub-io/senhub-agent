package proxmox

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func tagsMap(dp data_store.DataPoint) map[string]string {
	m := make(map[string]string)
	for _, tag := range dp.Tags {
		m[tag.Key] = tag.Value
	}
	return m
}

func TestCollect_CheckResultAndIdentityKeysSuccess(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var checkFound bool
	var checkValue float64
	for _, dp := range points {
		tags := tagsMap(dp)
		id, ok := tags["service.instance.id"]
		if !ok {
			t.Errorf("datapoint %q missing service.instance.id tag", dp.Name)
			continue
		}
		if id == "" {
			t.Errorf("datapoint %q has empty service.instance.id tag", dp.Name)
		}
		if id != "proxmox:test-cluster" {
			t.Errorf("datapoint %q service.instance.id = %q, want %q", dp.Name, id, "proxmox:test-cluster")
		}

		if dp.Name == dbcommon.CheckResultMetric {
			checkFound = true
			checkValue = dp.Value
			if tags["metric_type"] != "status" {
				t.Errorf("check result metric_type = %q, want status", tags["metric_type"])
			}
		}
	}
	if !checkFound {
		t.Fatal("senhub.probe.check.result was not emitted")
	}
	if checkValue != 0 {
		t.Errorf("successful check result value = %v, want 0", checkValue)
	}
}

func TestCollect_CheckResultAndIdentityKeysAPIError(t *testing.T) {
	srv := newTestServer(t)
	probe, err := NewProxmoxProbe(map[string]interface{}{
		"endpoint":     srv.URL,
		"token_id":     "root@pam!mon",
		"token_secret": "secret",
		"verify_tls":   false,
	}, testLogger())
	if err != nil {
		srv.Close()
		t.Fatalf("NewProxmoxProbe: %v", err)
	}

	p := probe.(*ProxmoxProbe)
	// Pin the fallback identity as a previous successful standalone cycle
	// would; the failed connection must still carry the entity key.
	p.entitySrc.refresh("")
	id := p.entitySrc.InstanceID()
	if id == "" {
		srv.Close()
		t.Fatal("expected fallback entity id to be pinned before failure")
	}

	srv.Close()
	p.client.CloseIdleConnections()

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var upFound, checkFound bool
	var checkValue float64
	for _, dp := range points {
		tags := tagsMap(dp)
		gotID, ok := tags["service.instance.id"]
		if !ok {
			t.Errorf("datapoint %q missing service.instance.id tag", dp.Name)
			continue
		}
		if gotID != id {
			t.Errorf("datapoint %q service.instance.id = %q, want %q", dp.Name, gotID, id)
		}

		switch dp.Name {
		case "senhub.proxmox.up":
			upFound = true
			if dp.Value != 0 {
				t.Errorf("senhub.proxmox.up = %v, want 0 on API error", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			checkFound = true
			checkValue = dp.Value
			if tags["metric_type"] != "status" {
				t.Errorf("check result metric_type = %q, want status", tags["metric_type"])
			}
		}
	}
	if !upFound {
		t.Error("senhub.proxmox.up was not emitted")
	}
	if !checkFound {
		t.Fatal("senhub.probe.check.result was not emitted")
	}
	if checkValue != float64(dbcommon.CheckTargetDown) {
		t.Errorf("API error check result = %v, want %v (target down)", checkValue, float64(dbcommon.CheckTargetDown))
	}
}

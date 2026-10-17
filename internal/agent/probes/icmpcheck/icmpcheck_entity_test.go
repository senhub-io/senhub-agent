package icmpcheck

import (
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func findTag(tags []tags.Tag, key string) (string, bool) {
	for _, tg := range tags {
		if tg.Key == key {
			return tg.Value, true
		}
	}
	return "", false
}

func TestBuildEntityObservation(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"192.0.2.10"}})
	p.entitySrc.hostID = "host-1"

	cases := []struct {
		name          string
		results       []pingResult
		agentID       string
		wantEntities  int
		wantRelations int
		wantType      string
		wantID        map[string]any
		wantAttr      string
	}{
		{
			name:    "loopback target emits nothing",
			results: []pingResult{{target: "127.0.0.1", resolvedIP: "127.0.0.1"}},
			agentID: "agent-1",
		},
		{
			name:          "remote IP emits network.address and monitors",
			results:       []pingResult{{target: "192.0.2.10"}},
			agentID:       "agent-1",
			wantEntities:  1,
			wantRelations: 1,
			wantType:      "network.address",
			wantID:        map[string]any{"network.address": "192.0.2.10"},
			wantAttr:      "host-1",
		},
		{
			name:          "remote hostname resolved emits canonical IP identity",
			results:       []pingResult{{target: "host.example", resolvedIP: "192.0.2.20"}},
			agentID:       "agent-1",
			wantEntities:  1,
			wantRelations: 1,
			wantType:      "network.address",
			wantID:        map[string]any{"network.address": "192.0.2.20"},
			wantAttr:      "host-1",
		},
		{
			name:          "empty agent id emits no monitors relation",
			results:       []pingResult{{target: "192.0.2.10"}},
			agentID:       "",
			wantEntities:  1,
			wantRelations: 0,
			wantType:      "network.address",
			wantID:        map[string]any{"network.address": "192.0.2.10"},
			wantAttr:      "host-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := p.buildEntityObservation(tc.results, tc.agentID)
			if len(obs.Entities) != tc.wantEntities {
				t.Fatalf("entities = %d, want %d", len(obs.Entities), tc.wantEntities)
			}
			if len(obs.Relations) != tc.wantRelations {
				t.Fatalf("relations = %d, want %d", len(obs.Relations), tc.wantRelations)
			}
			if tc.wantEntities == 0 {
				return
			}

			ent := obs.Entities[0]
			if ent.Type != tc.wantType {
				t.Errorf("entity type = %q, want %q", ent.Type, tc.wantType)
			}
			if ent.ID["network.address"] != tc.wantID["network.address"] {
				t.Errorf("network.address = %v, want %v", ent.ID["network.address"], tc.wantID["network.address"])
			}
			if got := ent.Attributes["senhub.check.probe_host.id"]; got != tc.wantAttr {
				t.Errorf("probe_host.id attr = %v, want %v", got, tc.wantAttr)
			}

			if tc.wantRelations == 0 {
				return
			}
			rel := obs.Relations[0]
			if rel.Type != "monitors" {
				t.Errorf("relation type = %q, want monitors", rel.Type)
			}
			if rel.FromType != "service.instance" || rel.FromID["service.instance.id"] != tc.agentID {
				t.Errorf("relation source = %s/%v, want service.instance/%q", rel.FromType, rel.FromID["service.instance.id"], tc.agentID)
			}
			if rel.ToType != tc.wantType || rel.ToID["network.address"] != tc.wantID["network.address"] {
				t.Errorf("relation target = %s/%v, want %s/%v", rel.ToType, rel.ToID["network.address"], tc.wantType, tc.wantID["network.address"])
			}
		})
	}
}

func TestCollectAddsEntityIdentityTagsAndCheckResult(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"192.0.2.10"}})
	p.entitySrc.hostID = "host-1"
	p.ping = func(target string) pingResult {
		return pingResult{
			target:     target,
			resolvedIP: "192.0.2.10",
			sent:       1,
			received:   1,
			lossRatio:  0,
			minRTT:     time.Millisecond,
			avgRTT:     time.Millisecond,
			maxRTT:     time.Millisecond,
			stddevRTT:  time.Millisecond,
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var sawCheckResult bool
	for _, dp := range points {
		if got, ok := findTag(dp.Tags, "network.address"); !ok || got != "192.0.2.10" {
			t.Errorf("datapoint %s missing network.address identity tag: %+v", dp.Name, dp.Tags)
		}
		if got, ok := findTag(dp.Tags, "target"); !ok || got != "192.0.2.10" {
			t.Errorf("datapoint %s missing target tag: %+v", dp.Name, dp.Tags)
		}

		if dp.Name == dbcommon.CheckResultMetric {
			sawCheckResult = true
			if got, ok := findTag(dp.Tags, "metric_type"); !ok || got != "status" {
				t.Errorf("check result metric_type = %v (present=%v), want status", got, ok)
			}
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("check result value = %v, want %v", dp.Value, dbcommon.CheckOK)
			}
		}
	}
	if !sawCheckResult {
		t.Error("missing check result datapoint")
	}
}

func TestCheckResultClassification_RefusedConnection(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"192.0.2.10"}})
	p.entitySrc.hostID = "host-1"

	res := pingResult{
		target:     "192.0.2.10",
		resolvedIP: "192.0.2.10",
		err:        errors.New("connection refused"),
	}
	identity := p.identityTagsForResult(res)
	dp := p.buildCheckResult(res, time.Now(), identity)

	if dp.Name != dbcommon.CheckResultMetric {
		t.Fatalf("metric name = %q, want %q", dp.Name, dbcommon.CheckResultMetric)
	}
	if dp.Value != float64(dbcommon.CheckTargetDown) {
		t.Fatalf("check result value = %v, want %v", dp.Value, dbcommon.CheckTargetDown)
	}
	if got, ok := findTag(dp.Tags, "metric_type"); !ok || got != "status" {
		t.Errorf("metric_type = %v (present=%v), want status", got, ok)
	}
	if got, ok := findTag(dp.Tags, "network.address"); !ok || got != "192.0.2.10" {
		t.Errorf("network.address = %v (present=%v), want 192.0.2.10", got, ok)
	}
}

func TestEntitySourceReadiness(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"192.0.2.10"}})
	p.entitySrc.hostID = "host-1"

	if _, ok := p.entitySrc.Observe(); ok {
		t.Fatal("entity source ready before first Collect")
	}

	p.ping = func(target string) pingResult {
		return pingResult{target: target, sent: 1, received: 1}
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("entity source not ready after Collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(obs.Entities))
	}
}

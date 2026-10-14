package httpcheck

import (
	"net"
	"strings"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

func newTestEntitySource(hostID, agentID string) *httpEntitySource {
	return &httpEntitySource{
		hostID:  hostID,
		agentID: func() string { return agentID },
	}
}

func TestHTTPCheckEntitySource_Observation(t *testing.T) {
	tests := []struct {
		name               string
		target             string
		hostID             string
		agentID            string
		wantEntityType     string
		wantID             map[string]any
		wantRelation       bool
		wantRelationToType string
	}{
		{
			name:           "localhost target maps to the local listener",
			target:         "http://localhost/health",
			hostID:         "host-1",
			agentID:        "agent-1",
			wantEntityType: "",
			wantID: map[string]any{
				"service.endpoint": entity.ListenerEndpoint("host-1", 80, "tcp"),
			},
			wantRelation:       true,
			wantRelationToType: "service.listener",
		},
		{
			name:           "remote IP target emits network.endpoint",
			target:         "http://192.0.2.10:8080/",
			hostID:         "host-1",
			agentID:        "agent-1",
			wantEntityType: "network.endpoint",
			wantID: map[string]any{
				"server.address":    "192.0.2.10",
				"server.port":       "8080",
				"network.transport": "tcp",
			},
			wantRelation:       true,
			wantRelationToType: "network.endpoint",
		},
		{
			name:           "remote DNS name is lowercased in the endpoint key",
			target:         "http://Example.COM:443/path",
			hostID:         "host-1",
			agentID:        "agent-1",
			wantEntityType: "network.endpoint",
			wantID: map[string]any{
				"server.address":    "example.com",
				"server.port":       "443",
				"network.transport": "tcp",
			},
			wantRelation:       true,
			wantRelationToType: "network.endpoint",
		},
		{
			name:           "empty agentID skips monitors but still emits target entity",
			target:         "http://192.0.2.10:8080/",
			hostID:         "host-1",
			agentID:        "",
			wantEntityType: "network.endpoint",
			wantID: map[string]any{
				"server.address":    "192.0.2.10",
				"server.port":       "8080",
				"network.transport": "tcp",
			},
			wantRelation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTestEntitySource(tt.hostID, tt.agentID)
			infos := src.rebuild([]string{tt.target})
			obs, ok := src.Observe()
			if !ok {
				t.Fatal("Observe returned false after first Collect")
			}

			info := infos[tt.target]
			if !equalID(info.id, tt.wantID) {
				t.Fatalf("series identity = %#v, want %#v", info.id, tt.wantID)
			}

			if tt.wantEntityType == "" {
				if len(obs.Entities) != 0 {
					t.Fatalf("entities = %v, want no emitted entity for local target", obs.Entities)
				}
			} else {
				if len(obs.Entities) != 1 {
					t.Fatalf("entities = %v, want one %s", obs.Entities, tt.wantEntityType)
				}
				got := obs.Entities[0]
				if got.Type != tt.wantEntityType {
					t.Fatalf("entity type = %s, want %s", got.Type, tt.wantEntityType)
				}
				if !equalID(got.ID, tt.wantID) {
					t.Fatalf("entity ID = %#v, want %#v", got.ID, tt.wantID)
				}
			}

			if tt.wantRelation {
				if len(obs.Relations) != 1 {
					t.Fatalf("relations = %v, want one", obs.Relations)
				}
				rel := obs.Relations[0]
				if rel.FromType != "service.instance" || rel.ToType != tt.wantRelationToType {
					t.Fatalf("relation = %#v, want monitors from service.instance to %s", rel, tt.wantRelationToType)
				}
				if !equalID(rel.ToID, tt.wantID) {
					t.Fatalf("relation target ID = %#v, want %#v", rel.ToID, tt.wantID)
				}
			} else if len(obs.Relations) != 0 {
				t.Fatalf("relations = %v, want none when agentID is empty", obs.Relations)
			}
		})
	}
}

func TestHTTPCheckCollect_SeriesTagsAndCheckResult(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{
			"http://192.0.2.10:8080",
			"http://example.com:443/health",
		},
	})
	p.entitySrc.hostID = "host-1"
	p.entitySrc.agentID = func() string { return "agent-1" }

	p.check = func(target string) httpResult {
		if strings.Contains(target, "192.0.2.10") {
			return httpResult{target: target, err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: syscall.ECONNREFUSED,
			}}
		}
		return httpResult{target: target, statusCode: 401, up: false}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertDatapointTag := func(target, key, want string) {
		t.Helper()
		for _, dp := range points {
			if !hasTag(dp.Tags, "target", target) {
				continue
			}
			if !hasTag(dp.Tags, key, want) {
				t.Fatalf("datapoint for target %q missing tag %s=%s: %v", target, key, want, dp.Tags)
			}
		}
	}

	assertDatapointTag("http://192.0.2.10:8080", "url.full", "http://192.0.2.10:8080")
	assertDatapointTag("http://192.0.2.10:8080", "server.address", "192.0.2.10")
	assertDatapointTag("http://192.0.2.10:8080", "server.port", "8080")
	assertDatapointTag("http://192.0.2.10:8080", "network.transport", "tcp")

	assertDatapointTag("http://example.com:443/health", "url.full", "http://example.com:443/health")
	assertDatapointTag("http://example.com:443/health", "server.address", "example.com")
	assertDatapointTag("http://example.com:443/health", "server.port", "443")
	assertDatapointTag("http://example.com:443/health", "network.transport", "tcp")

	checkValues := map[string]float64{}
	for _, dp := range points {
		if dp.Name != "senhub.probe.check.result" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "target" {
				checkValues[tg.Value] = dp.Value
			}
		}
	}

	if got := checkValues["http://192.0.2.10:8080"]; got != 1 {
		t.Errorf("check result for refused connection = %v, want 1", got)
	}
	if got := checkValues["http://example.com:443/health"]; got != 2 {
		t.Errorf("check result for HTTP 401 = %v, want 2", got)
	}
}

func equalID(got, want map[string]any) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func hasTag(tags []tags.Tag, key, want string) bool {
	for _, tg := range tags {
		if tg.Key == key && tg.Value == want {
			return true
		}
	}
	return false
}

var _ = data_store.DataPoint{}

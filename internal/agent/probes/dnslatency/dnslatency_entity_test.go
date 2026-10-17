package dnslatency

import (
	"errors"
	"reflect"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

func TestDNSEntitySourceTargets(t *testing.T) {
	tests := []struct {
		name           string
		resolver       string
		hostID         string
		agentID        string
		wantEntities   int
		wantRelations  int
		wantEntityType string
		wantEntityID   map[string]any

		wantRelationToType string
		wantRelationToID   map[string]any
	}{
		{
			name:               "localhost target is only a monitors relation to service.listener",
			resolver:           "localhost:53",
			hostID:             "host-1",
			agentID:            "agent-1",
			wantEntities:       0,
			wantRelations:      1,
			wantRelationToType: "service.listener",
			wantRelationToID:   map[string]any{"service.endpoint": entity.ListenerEndpoint("host-1", 53, "udp")},
		},
		{
			name:           "remote IP emits network.endpoint plus monitors",
			resolver:       "192.0.2.53:53",
			hostID:         "host-1",
			agentID:        "agent-1",
			wantEntities:   1,
			wantRelations:  1,
			wantEntityType: "network.endpoint",
			wantEntityID: map[string]any{
				"server.address":    "192.0.2.53",
				"server.port":       "53",
				"network.transport": "udp",
			},
			wantRelationToType: "network.endpoint",
			wantRelationToID: map[string]any{
				"server.address":    "192.0.2.53",
				"server.port":       "53",
				"network.transport": "udp",
			},
		},
		{
			name:           "remote DNS name endpoint key uses lowercased configured name",
			resolver:       "Resolver.Example.COM:53",
			hostID:         "host-1",
			agentID:        "agent-1",
			wantEntities:   1,
			wantRelations:  1,
			wantEntityType: "network.endpoint",
			wantEntityID: map[string]any{
				"server.address":    "resolver.example.com",
				"server.port":       "53",
				"network.transport": "udp",
			},
			wantRelationToType: "network.endpoint",
			wantRelationToID: map[string]any{
				"server.address":    "resolver.example.com",
				"server.port":       "53",
				"network.transport": "udp",
			},
		},
		{
			name:           "empty agent ID skips monitors",
			resolver:       "192.0.2.53:53",
			hostID:         "host-1",
			agentID:        "",
			wantEntities:   1,
			wantRelations:  0,
			wantEntityType: "network.endpoint",
			wantEntityID: map[string]any{
				"server.address":    "192.0.2.53",
				"server.port":       "53",
				"network.transport": "udp",
			},
		},
		{
			name:          "system resolver has no entity",
			resolver:      systemResolverLabel,
			hostID:        "host-1",
			agentID:       "agent-1",
			wantEntities:  0,
			wantRelations: 0,
		},
		{
			name:          "local target with unknown host ID has no relation and no identity key",
			resolver:      "localhost:53",
			hostID:        "",
			agentID:       "agent-1",
			wantEntities:  0,
			wantRelations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newDNSEntitySource(tt.hostID)
			src.rebuild([]string{tt.resolver}, tt.agentID)

			obs, ok := src.Observe()
			if !ok {
				t.Fatal("Observe returned ok=false after rebuild")
			}
			if len(obs.Entities) != tt.wantEntities {
				t.Fatalf("entities = %d, want %d", len(obs.Entities), tt.wantEntities)
			}
			if len(obs.Relations) != tt.wantRelations {
				t.Fatalf("relations = %d, want %d", len(obs.Relations), tt.wantRelations)
			}

			if tt.wantEntityType != "" {
				gotEntity := obs.Entities[0]
				if gotEntity.Type != tt.wantEntityType {
					t.Errorf("entity type = %q, want %q", gotEntity.Type, tt.wantEntityType)
				}
				if !reflect.DeepEqual(gotEntity.ID, tt.wantEntityID) {
					t.Errorf("entity ID = %#v, want %#v", gotEntity.ID, tt.wantEntityID)
				}
			}

			if tt.wantRelationToType != "" {
				gotRelation := obs.Relations[0]
				if gotRelation.Type != "monitors" {
					t.Errorf("relation type = %q, want monitors", gotRelation.Type)
				}
				if gotRelation.FromType != "service.instance" {
					t.Errorf("relation from type = %q, want service.instance", gotRelation.FromType)
				}
				if !reflect.DeepEqual(gotRelation.FromID, map[string]any{"service.instance.id": tt.agentID}) {
					t.Errorf("relation from ID = %#v", gotRelation.FromID)
				}
				if gotRelation.ToType != tt.wantRelationToType {
					t.Errorf("relation to type = %q, want %q", gotRelation.ToType, tt.wantRelationToType)
				}
				if !reflect.DeepEqual(gotRelation.ToID, tt.wantRelationToID) {
					t.Errorf("relation to ID = %#v, want %#v", gotRelation.ToID, tt.wantRelationToID)
				}
			}
		})
	}
}

func TestCollectRemoteIPTagsAndCheckResult(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"names":     []interface{}{"example.com"},
		"resolvers": []interface{}{"192.0.2.1"},
	})
	p.entitySrc = newDNSEntitySource("host-1")
	p.SetEntitySource(p.entitySrc)

	p.lookup = func(name, resolver string) lookupResult {
		return lookupResult{
			name:     name,
			resolver: resolver,
			err:      errors.New("dial udp 192.0.2.1:53: connect: connection refused"),
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	var upFound, checkFound bool
	for _, dp := range points {
		switch dp.Name {
		case "senhub.dns.up":
			upFound = true
			tm := tagMap(dp.Tags)
			if tm["server.address"] != "192.0.2.1" {
				t.Errorf("dns.up server.address = %q, want 192.0.2.1", tm["server.address"])
			}
			if tm["server.port"] != "53" {
				t.Errorf("dns.up server.port = %q, want 53", tm["server.port"])
			}
			if tm["network.transport"] != "udp" {
				t.Errorf("dns.up network.transport = %q, want udp", tm["network.transport"])
			}
		case dbcommon.CheckResultMetric:
			checkFound = true
			tm := tagMap(dp.Tags)
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("check result value = %v, want %v", dp.Value, float64(dbcommon.CheckTargetDown))
			}
			if tm["metric_type"] != "status" {
				t.Errorf("check result metric_type = %q, want status", tm["metric_type"])
			}
			if tm["server.address"] != "192.0.2.1" {
				t.Errorf("check result server.address = %q, want 192.0.2.1", tm["server.address"])
			}
			if tm["server.port"] != "53" {
				t.Errorf("check result server.port = %q, want 53", tm["server.port"])
			}
			if tm["network.transport"] != "udp" {
				t.Errorf("check result network.transport = %q, want udp", tm["network.transport"])
			}
		}
	}
	if !upFound {
		t.Error("no senhub.dns.up point found")
	}
	if !checkFound {
		t.Error("no check result point found")
	}
}

func tagMap(tags []tags.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, tag := range tags {
		m[tag.Key] = tag.Value
	}
	return m
}

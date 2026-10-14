package ntp

import (
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
)

func pointForServer(t *testing.T, points []data_store.DataPoint, name, server string) data_store.DataPoint {
	t.Helper()
	for _, p := range points {
		if p.Name != name {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == "server" && tag.Value == server {
				return p
			}
		}
	}
	t.Fatalf("no datapoint %q for server %q", name, server)
	return data_store.DataPoint{}
}

func tagValue(p data_store.DataPoint, key string) (string, bool) {
	for _, tag := range p.Tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func TestNTPEntitySourceAndCheckResult(t *testing.T) {
	refusedErr := &net.OpError{Op: "read", Net: "udp", Err: syscall.ECONNREFUSED}

	tests := []struct {
		name     string
		server   string
		hostID   string
		agentID  string
		queryErr error
		assert   func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint)
	}{
		{
			name:    "localhost target only relation to listener",
			server:  "localhost",
			hostID:  "host-1",
			agentID: "agent-1",
			assert: func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint) {
				obs, ok := s.Observe()
				if !ok {
					t.Fatal("Observe returned false after first Collect")
				}
				if len(obs.Entities) != 0 {
					t.Fatalf("local target must not emit the listener entity; got %d entities", len(obs.Entities))
				}
				if len(obs.Relations) != 1 {
					t.Fatalf("relations = %d, want 1", len(obs.Relations))
				}
				rel := obs.Relations[0]
				wantListenerID := entity.ListenerEndpoint("host-1", 123, "udp")
				if rel.Type != "monitors" || rel.ToType != "service.listener" || rel.ToID["service.endpoint"] != wantListenerID {
					t.Fatalf("relation = %#v, want monitors to service.listener %q", rel, wantListenerID)
				}

				up := pointForServer(t, points, "senhub.ntp.up", "localhost")
				got, ok := tagValue(up, "service.endpoint")
				if !ok || got != wantListenerID {
					t.Fatalf("series identity tag = %q, %v; want %q", got, ok, wantListenerID)
				}
			},
		},
		{
			name:    "remote IP emits network.endpoint",
			server:  "192.0.2.10",
			hostID:  "host-1",
			agentID: "agent-1",
			assert: func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint) {
				obs, ok := s.Observe()
				if !ok {
					t.Fatal("Observe returned false after first Collect")
				}
				if len(obs.Entities) != 1 {
					t.Fatalf("entities = %d, want 1", len(obs.Entities))
				}
				ent := obs.Entities[0]
				if ent.Type != "network.endpoint" {
					t.Fatalf("entity type = %q, want network.endpoint", ent.Type)
				}
				if got := ent.ID["server.address"]; got != "192.0.2.10" {
					t.Errorf("server.address = %v, want 192.0.2.10", got)
				}
				if got := ent.ID["server.port"]; got != "123" {
					t.Errorf("server.port = %v, want 123", got)
				}
				if got := ent.ID["network.transport"]; got != "udp" {
					t.Errorf("network.transport = %v, want udp", got)
				}

				if len(obs.Relations) != 1 {
					t.Fatalf("relations = %d, want 1", len(obs.Relations))
				}
				rel := obs.Relations[0]
				if rel.Type != "monitors" || rel.ToType != "network.endpoint" {
					t.Fatalf("relation = %#v, want monitors to network.endpoint", rel)
				}

				up := pointForServer(t, points, "senhub.ntp.up", "192.0.2.10")
				for _, key := range []string{"server.address", "server.port", "network.transport"} {
					if _, ok := tagValue(up, key); !ok {
						t.Errorf("series missing identity tag %q", key)
					}
				}
			},
		},
		{
			name:    "remote DNS name lowercased in endpoint key",
			server:  "Ntp.Example.Org",
			hostID:  "host-1",
			agentID: "agent-1",
			assert: func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint) {
				obs, ok := s.Observe()
				if !ok {
					t.Fatal("Observe returned false after first Collect")
				}
				if len(obs.Entities) != 1 {
					t.Fatalf("entities = %d, want 1", len(obs.Entities))
				}
				got := obs.Entities[0].ID["server.address"]
				if got != "ntp.example.org" {
					t.Errorf("endpoint key = %v, want lowercased ntp.example.org", got)
				}

				up := pointForServer(t, points, "senhub.ntp.up", "Ntp.Example.Org")
				if v, ok := tagValue(up, "server.address"); !ok || v != "ntp.example.org" {
					t.Errorf("series server.address = %q, %v; want ntp.example.org", v, ok)
				}
			},
		},
		{
			name:    "agentID empty skips relation but not entity",
			server:  "192.0.2.10",
			hostID:  "host-1",
			agentID: "",
			assert: func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint) {
				obs, ok := s.Observe()
				if !ok {
					t.Fatal("Observe returned false after first Collect")
				}
				if len(obs.Entities) != 1 {
					t.Fatalf("entities = %d, want 1", len(obs.Entities))
				}
				if len(obs.Relations) != 0 {
					t.Fatalf("relations = %d, want 0 when agentID is empty", len(obs.Relations))
				}
			},
		},
		{
			name:    "refused connection check result is target down",
			server:  "192.0.2.10",
			hostID:  "host-1",
			agentID: "agent-1",
			queryErr: fmt.Errorf("%w: waiting for %s: %w",
				errUnreachable, "192.0.2.10:123", refusedErr),
			assert: func(t *testing.T, s *ntpEntitySource, points []data_store.DataPoint) {
				cr := pointForServer(t, points, dbcommon.CheckResultMetric, "192.0.2.10")
				if cr.Value != 1 {
					t.Errorf("check result = %v, want 1 for refused connection", cr.Value)
				}
				if v, ok := tagValue(cr, "metric_type"); !ok || v != "status" {
					t.Errorf("check result metric_type = %q, %v; want status", v, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProbe(t, map[string]interface{}{
				"servers": []interface{}{tt.server},
				"samples": 1,
			})
			p.entitySrc.hostID = tt.hostID
			p.entitySrc.agentID = func() string { return tt.agentID }
			p.query = func(server string, _ time.Duration) (sample, error) {
				if tt.queryErr != nil {
					return sample{}, tt.queryErr
				}
				return sample{
					offset:    time.Millisecond,
					roundTrip: time.Millisecond,
					stratum:   2,
				}, nil
			}

			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			tt.assert(t, p.entitySrc, points)
		})
	}
}

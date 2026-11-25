package otlp

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
)

// The agent key opens the agent's HTTP API, so it must never travel as
// telemetry: every backend reader would hold it. The resource carries a
// one-way UUID derived from it instead (ent#95).
func TestResourceNeverCarriesTheAgentKey(t *testing.T) {
	const key = "test-key-12345678-abcdef"
	s := newTestStrategy(t, nil)

	if got, want := s.cfg.Resource.ServiceInstance, configuration.AgentInstanceID(key); got != want {
		t.Errorf("service.instance.id = %q, want the derived id %q", got, want)
	}
	for _, res := range []struct {
		name  string
		attrs []string
	}{
		{"resource", resourceValues(s)},
		{"metrics resource", metricsResourceValues(s)},
	} {
		for _, v := range res.attrs {
			if strings.Contains(v, key) {
				t.Errorf("%s carries the agent key in %q", res.name, v)
			}
		}
	}
	if id := agentSelfIdentity(s.cfg); strings.Contains(id.InstanceID, key) {
		t.Errorf("agent entity identity carries the agent key: %q", id.InstanceID)
	}
}

func resourceValues(s *OTLPSyncStrategy) []string {
	var out []string
	if s.resource == nil {
		return out
	}
	for _, kv := range s.resource.Attributes() {
		out = append(out, string(kv.Key)+"="+kv.Value.String())
	}
	return out
}

func metricsResourceValues(s *OTLPSyncStrategy) []string {
	var out []string
	if s.metricsResource == nil {
		return out
	}
	for _, kv := range s.metricsResource.Attributes() {
		out = append(out, string(kv.Key)+"="+kv.Value.String())
	}
	return out
}

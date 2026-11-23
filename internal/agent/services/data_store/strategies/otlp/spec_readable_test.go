package otlp

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
)

func TestOTLPSpecKeepsTheTenantHeaderReadable(t *testing.T) {
	o, ok := outputspec.For("otlp")
	if !ok {
		t.Fatal("otlp output spec not registered")
	}
	paths := map[string]bool{}
	for _, p := range o.SecretPaths() {
		paths[p] = true
	}
	for _, path := range []string{"headers", "signals.metrics.headers", "signals.logs.headers"} {
		if !paths[path] {
			t.Errorf("%s is not declared secret", path)
		}
	}
	got := spec.Probe{Type: o.Type, Params: o.Params}.ReadableKeysAt("headers")
	if len(got) != 1 || got[0] != "X-Scope-OrgID" {
		t.Errorf("readable keys of headers = %v, want [X-Scope-OrgID]", got)
	}
}

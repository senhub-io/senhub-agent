package transformers

import (
	"fmt"
	"sync"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestTransformerRegistry_ConcurrentAccess reproduces the #259 access
// pattern under the race detector: probe scheduler goroutines loading
// transformers (cache writes on first load), HTTP scrape handlers
// resolving probe definitions, and OTLP-style per-series definition
// lookups — all concurrently. The historical registry had no
// synchronization: this pattern was a `fatal: concurrent map writes`
// waiting for the startup first-load window.
func TestTransformerRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewTransformerRegistry(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))

	probes := []string{"cpu", "memory", "network", "logicaldisk", "snmp_poll", "mysql", "postgresql", "redfish"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		// Writers: first-load transformers (probe schedulers at startup).
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				probe := probes[(n+j)%len(probes)]
				if _, err := registry.LoadTransformer(probe, "friendly"); err != nil {
					t.Errorf("LoadTransformer(%s): %v", probe, err)
					return
				}
				// Unknown probe types exercise the fallback path.
				if _, err := registry.LoadTransformer(fmt.Sprintf("custom_%d", n), "friendly"); err != nil {
					t.Errorf("LoadTransformer(custom): %v", err)
					return
				}
			}
		}(i)

		// Readers: definition lookups (Prometheus scrape + OTLP export),
		// including memoized negatives.
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				probe := probes[(n+j)%len(probes)]
				if def := registry.GetProbeDefinition(probe); def == nil {
					t.Errorf("GetProbeDefinition(%s) returned nil for an embedded probe", probe)
					return
				}
				_ = registry.GetProbeDefinition("does_not_exist")
			}
		}(i)
	}
	wg.Wait()
}

// TestTransformerRegistry_EagerDefinitions pins the #259 performance
// contract: embedded definitions are parsed once at construction and
// every GetProbeDefinition afterwards is an index hit — the OTLP
// export path must never re-parse YAML per series.
func TestTransformerRegistry_EagerDefinitions(t *testing.T) {
	registry := NewTransformerRegistry(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))

	if len(registry.definitions) == 0 {
		t.Fatal("registry constructed with no eager definitions")
	}
	first := registry.GetProbeDefinition("cpu")
	second := registry.GetProbeDefinition("cpu")
	if first == nil || second == nil {
		t.Fatal("embedded cpu definition not found")
	}
	if first != second {
		t.Error("GetProbeDefinition returned distinct instances — definition is re-parsed instead of served from the index")
	}

	// Negative lookups are memoized too.
	if registry.GetProbeDefinition("nope") != nil {
		t.Error("unknown probe returned a definition")
	}
	registry.mu.RLock()
	_, memoized := registry.definitions["nope"]
	registry.mu.RUnlock()
	if !memoized {
		t.Error("negative lookup was not memoized")
	}
}
